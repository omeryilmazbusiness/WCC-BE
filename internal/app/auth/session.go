package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// SessionActor is the signed-in caller managing their own sessions.
type SessionActor struct {
	UserID           uuid.UUID
	CurrentSessionID uuid.UUID
	IP               string
	UserAgent        string
}

type RevokeSessionInput struct {
	SessionActor
	SessionID uuid.UUID
}

type AdminRevokeSessionsInput struct {
	ActorID   uuid.UUID
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

// completeLogin runs after every factor succeeded and starts a new session.
func (s *Service) completeLogin(ctx context.Context, user *identity.User, email string, meta requestMeta, method string) (*platformauth.TokenPair, error) {
	s.clearFailures(ctx, user.ID, email)
	now := s.now()
	idle, absolute := s.opts.Session.Start(now)
	sess := &authsec.Session{
		ID: uuid.New(), UserID: user.ID, CreatedAt: now, AbsoluteExpiresAt: absolute, IdleExpiresAt: idle,
		LastSeenAt: now, LastIP: meta.IP, UserAgent: truncateUA(meta.UserAgent), AuthMethod: sessionAuthMethod(method),
	}
	var pair *platformauth.TokenPair
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.sessions.CreateSession(ctx, sess); err != nil {
			return err
		}
		p, _, err := s.issueTokens(ctx, user, sess.ID, idle, meta)
		pair = p
		return err
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, user.ID, user, "auth.login", meta, map[string]any{"method": method, "session_id": sess.ID})
	return pair, nil
}

// Refresh rotates a refresh token within its session. Tokens are single-use:
// replaying a rotated one ends the session, except inside the reuse grace
// window where it yields a sibling token for the racing request.
func (s *Service) Refresh(ctx context.Context, in RefreshInput) (*platformauth.TokenPair, error) {
	raw := strings.TrimSpace(in.RefreshToken)
	if !authsec.IsRefreshToken(raw) {
		return nil, errInvalidRefresh
	}
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	hash := authsec.HashToken(raw)
	pair, err := s.refreshOnce(ctx, hash, meta)
	if errors.Is(err, errRefreshRaced) {
		// Rotated by a concurrent request between read and write: re-evaluate,
		// which lands in the grace window or in reuse handling.
		pair, err = s.refreshOnce(ctx, hash, meta)
	}
	if errors.Is(err, errRefreshRaced) {
		return nil, errInvalidRefresh
	}
	return pair, err
}

func (s *Service) refreshOnce(ctx context.Context, hash string, meta requestMeta) (*platformauth.TokenPair, error) {
	rec, err := s.refresh.FindRefreshToken(ctx, hash)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, errInvalidRefresh
	}
	sess, err := s.sessions.FindSession(ctx, rec.FamilyID)
	if err != nil {
		return nil, err
	}
	if sess == nil || sess.UserID != rec.UserID {
		return nil, errInvalidRefresh
	}
	now := s.now()
	verdict := authsec.EvaluateRefresh(*rec, *sess, now, s.opts.ReuseGrace)
	switch verdict {
	case authsec.RefreshReused:
		s.endSession(ctx, rec.UserID, sess.ID, authsec.RevokeReuse, "auth.refresh_reuse", meta, map[string]any{"token_id": rec.ID})
		return nil, errInvalidRefresh
	case authsec.RefreshExpired:
		s.endSession(ctx, rec.UserID, sess.ID, authsec.RevokeExpired, "auth.session_expired", meta, nil)
		return nil, errInvalidRefresh
	case authsec.RefreshRevoked:
		return nil, errInvalidRefresh
	}
	user, err := s.users.FindUserByID(ctx, rec.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		s.endSession(ctx, rec.UserID, sess.ID, authsec.RevokeUserDeactivated, "auth.session_revoked", meta, nil)
		return nil, errInvalidRefresh
	}

	expires := authsec.SuccessorExpiry(now, s.opts.Session.Idle, sess.AbsoluteExpiresAt)
	var pair *platformauth.TokenPair
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		p, tokenID, err := s.issueTokens(ctx, user, sess.ID, expires, meta)
		if err != nil {
			return err
		}
		if verdict == authsec.RefreshRotate {
			ok, err := s.refresh.MarkRefreshRotated(ctx, rec.ID, tokenID, now)
			if err != nil {
				return err
			}
			if !ok {
				return errRefreshRaced
			}
		}
		ok, err := s.sessions.TouchSession(ctx, sess.ID, expires, now, meta.IP)
		if err != nil {
			return err
		}
		if !ok {
			return errInvalidRefresh
		}
		pair = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	if verdict == authsec.RefreshGrace {
		s.recordSession(ctx, user.ID, sess.ID, "auth.refresh_grace", meta, map[string]any{
			"user_id": user.ID, "token_id": rec.ID, "rotated_at": rec.RevokedAt,
		})
	}
	return pair, nil
}

// Logout ends the caller's session and, when a refresh token is supplied,
// the session it belongs to.
func (s *Service) Logout(ctx context.Context, in LogoutInput) error {
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	userID := in.UserID
	targets := []uuid.UUID{in.SessionID}
	if tok := strings.TrimSpace(in.RefreshToken); authsec.IsRefreshToken(tok) {
		rec, err := s.refresh.FindRefreshToken(ctx, authsec.HashToken(tok))
		if err != nil {
			return err
		}
		if rec != nil && (userID == uuid.Nil || rec.UserID == userID) && rec.FamilyID != in.SessionID {
			targets = append(targets, rec.FamilyID)
			userID = rec.UserID
		}
	}
	if userID == uuid.Nil {
		return nil
	}
	now := s.now()
	revoked := []uuid.UUID{}
	for _, sid := range targets {
		if sid == uuid.Nil {
			continue
		}
		ok, err := s.sessions.RevokeSession(ctx, sid, authsec.RevokeLogout, now)
		s.cache.Invalidate(sid)
		if err != nil {
			return err
		}
		if ok {
			revoked = append(revoked, sid)
		}
	}
	id := userID
	var branch *uuid.UUID
	if in.BranchID != uuid.Nil {
		branch = &in.BranchID
	}
	return s.audit.Record(ctx, audit.RecordInput{
		ActorID: id, Action: "auth.logout", EntityType: "user", EntityID: &id, BranchID: branch,
		IP: meta.IP, UserAgent: meta.UserAgent,
		Extra: map[string]any{"session_id": in.SessionID, "revoked_sessions": revoked, "reason": authsec.RevokeLogout},
	})
}

// ListSessions returns the caller's active sessions, newest first.
func (s *Service) ListSessions(ctx context.Context, userID uuid.UUID) ([]authsec.Session, error) {
	return s.sessions.ListActiveSessions(ctx, userID, s.now())
}

// RevokeSession ends one of the caller's active sessions; ending the current
// one is a logout.
func (s *Service) RevokeSession(ctx context.Context, in RevokeSessionInput) error {
	sess, err := s.sessions.FindSession(ctx, in.SessionID)
	if err != nil {
		return err
	}
	now := s.now()
	if sess == nil || sess.UserID != in.UserID || !sess.Active(now) {
		return shared.NewNotFound("session")
	}
	reason := authsec.RevokeUser
	current := sess.ID == in.CurrentSessionID
	if current {
		reason = authsec.RevokeLogout
	}
	ok, err := s.sessions.RevokeSession(ctx, sess.ID, reason, now)
	s.cache.Invalidate(sess.ID)
	if err != nil {
		return err
	}
	if !ok {
		return shared.NewNotFound("session")
	}
	s.recordSession(ctx, in.UserID, sess.ID, "auth.session_revoked", requestMeta{IP: in.IP, UserAgent: in.UserAgent},
		map[string]any{"user_id": in.UserID, "reason": reason, "current": current})
	return nil
}

// RevokeOtherSessions ends every active session of the caller except the current one.
func (s *Service) RevokeOtherSessions(ctx context.Context, in SessionActor) (int, error) {
	ids, err := s.sessions.RevokeUserSessions(ctx, in.UserID, in.CurrentSessionID, authsec.RevokeUser, s.now())
	for _, id := range ids {
		s.cache.Invalidate(id)
	}
	if err != nil {
		return 0, err
	}
	id := in.UserID
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: id, Action: "auth.sessions_revoked", EntityType: "user", EntityID: &id,
		IP: in.IP, UserAgent: in.UserAgent,
		Extra: map[string]any{"session_ids": ids, "reason": authsec.RevokeUser, "kept_session_id": in.CurrentSessionID},
	})
	return len(ids), nil
}

// AdminRevokeSessions ends every active session of a user (PermUsersWrite).
func (s *Service) AdminRevokeSessions(ctx context.Context, in AdminRevokeSessionsInput) (int, error) {
	user, err := s.users.FindUserByID(ctx, in.UserID)
	if err != nil || user == nil || !access.From(ctx).CanAccessBranch(user.BranchID) {
		return 0, shared.NewNotFound("user")
	}
	ids, err := s.sessions.RevokeUserSessions(ctx, user.ID, uuid.Nil, authsec.RevokeAdmin, s.now())
	s.cache.InvalidateUser(user.ID)
	if err != nil {
		return 0, err
	}
	s.record(ctx, in.ActorID, user, "auth.sessions_revoked", requestMeta{IP: in.IP, UserAgent: in.UserAgent},
		map[string]any{"session_ids": ids, "reason": authsec.RevokeAdmin})
	return len(ids), nil
}

// issueTokens records a refresh token for session sid expiring at expires
// and signs an access token bound to it.
func (s *Service) issueTokens(ctx context.Context, user *identity.User, sid uuid.UUID, expires time.Time, meta requestMeta) (*platformauth.TokenPair, uuid.UUID, error) {
	raw, err := authsec.NewRefreshToken(rand.Reader)
	if err != nil {
		return nil, uuid.Nil, err
	}
	id := uuid.New()
	err = s.refresh.CreateRefreshToken(ctx, &authsec.RefreshToken{
		ID: id, UserID: user.ID, FamilyID: sid, TokenHash: authsec.HashToken(raw),
		ExpiresAt: expires, CreatedIP: meta.IP, UserAgent: truncateUA(meta.UserAgent), CreatedAt: s.now(),
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	teamID := uuid.Nil
	if user.TeamID != nil {
		teamID = *user.TeamID
	}
	at, err := s.tokens.IssueAccess(platformauth.AccessSubject{
		UserID: user.ID, Email: user.Email, Role: user.Role, BranchID: user.BranchID, TeamID: teamID,
		SessionID: sid, TokenVersion: user.TokenVersion,
	})
	if err != nil {
		return nil, uuid.Nil, err
	}
	return &platformauth.TokenPair{
		AccessToken: at.Token, RefreshToken: raw, ExpiresIn: at.ExpiresIn, RefreshExpiresAt: expires,
	}, id, nil
}

// endSession revokes a session on a refresh failure path; errors are
// swallowed because the caller already answers 401.
func (s *Service) endSession(ctx context.Context, userID, sid uuid.UUID, reason, action string, meta requestMeta, extra map[string]any) {
	ok, _ := s.sessions.RevokeSession(ctx, sid, reason, s.now())
	s.cache.Invalidate(sid)
	if extra == nil {
		extra = map[string]any{}
	}
	extra["user_id"], extra["reason"], extra["revoked"] = userID, reason, ok
	s.recordSession(ctx, userID, sid, action, meta, extra)
}

func (s *Service) recordSession(ctx context.Context, actor, sid uuid.UUID, action string, meta requestMeta, extra map[string]any) {
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: actor, Action: action, EntityType: "auth_session", EntityID: &sid,
		IP: meta.IP, UserAgent: meta.UserAgent, Extra: extra,
	})
}

func sessionAuthMethod(method string) string {
	switch method {
	case methodTOTP:
		return authsec.AuthMethodTOTP
	case methodRecovery:
		return authsec.AuthMethodRecovery
	case authsec.AuthMethodMFASetup:
		return authsec.AuthMethodMFASetup
	default:
		return authsec.AuthMethodPassword
	}
}

func truncateUA(ua string) string {
	if len(ua) > maxUserAgentLen {
		return ua[:maxUserAgentLen]
	}
	return ua
}
