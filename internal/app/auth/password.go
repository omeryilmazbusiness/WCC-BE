package auth

import (
	"context"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

const (
	passwordChangeWindow      = 15 * time.Minute
	passwordChangeMaxFailures = 5
)

type ChangePasswordInput struct {
	SessionActor
	CurrentPassword string
	NewPassword     string
}

func passwordChangeKey(userID uuid.UUID) string { return "password_change:" + userID.String() }

func passwordFieldError(field, reason, message string) error {
	err := shared.NewValidation(message)
	err.Details = map[string]any{field: reason}
	return err
}

// ChangePassword replaces the caller's password after re-checking the current
// one. The credential-change trigger ends every session, so this device gets a
// fresh session and token pair while all others are signed out.
func (s *Service) ChangePassword(ctx context.Context, in ChangePasswordInput) (*platformauth.TokenPair, error) {
	key := passwordChangeKey(in.UserID)
	if n, _ := s.limiter.Count(ctx, key, passwordChangeWindow); n >= passwordChangeMaxFailures {
		return nil, shared.NewRateLimited("too many password attempts", passwordChangeWindow)
	}
	user, _, err := s.loadUser(ctx, in.UserID)
	if err != nil {
		return nil, shared.NewNotFound("user")
	}
	if in.CurrentPassword == "" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.CurrentPassword)) != nil {
		_, _ = s.limiter.Hit(ctx, key, passwordChangeWindow)
		return nil, passwordFieldError("current_password", "incorrect", "current password is incorrect")
	}
	if err := platformauth.ValidatePassword(in.NewPassword); err != nil {
		return nil, passwordFieldError("new_password", "weak", err.Error())
	}
	if in.NewPassword == in.CurrentPassword {
		return nil, passwordFieldError("new_password", "unchanged", "new password must differ from the current one")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	now := s.now()
	idle, absolute := s.opts.Session.Start(now)
	sess := &authsec.Session{
		ID: uuid.New(), UserID: user.ID, CreatedAt: now, AbsoluteExpiresAt: absolute, IdleExpiresAt: idle,
		LastSeenAt: now, LastIP: meta.IP, UserAgent: truncateUA(meta.UserAgent), AuthMethod: authsec.AuthMethodPassword,
	}
	var pair *platformauth.TokenPair
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		user.PasswordHash, user.UpdatedAt = string(hash), now
		if err := s.users.UpdateUser(ctx, user); err != nil {
			return err
		}
		// Reload for the token_version the trigger just bumped.
		fresh, err := s.users.FindUserByID(ctx, user.ID)
		if err != nil {
			return err
		}
		if err := s.sessions.CreateSession(ctx, sess); err != nil {
			return err
		}
		pair, _, err = s.issueTokens(ctx, fresh, sess.ID, idle, meta)
		return err
	})
	if err != nil {
		return nil, err
	}
	s.cache.InvalidateUser(user.ID)
	_ = s.limiter.Reset(ctx, key)
	s.record(ctx, user.ID, user, "auth.password_changed", meta, map[string]any{
		"previous_session_id": in.CurrentSessionID, "session_id": sess.ID, "reason": authsec.RevokePasswordChanged,
	})
	return pair, nil
}
