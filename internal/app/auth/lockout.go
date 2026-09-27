package auth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// dummyHash equalizes bcrypt timing for unknown emails.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("wodi-timing-equalizer"), bcrypt.DefaultCost)
	return h
})

func ipKey(ip string) string { return "login:ip:" + ip }

// emailKey hashes the address so the rate-limit store holds no PII.
func emailKey(email string) string { return "login:email:" + authsec.HashToken(email) }

func lockedError(wait time.Duration) error {
	return shared.NewLocked("account temporarily locked", wait)
}

func (s *Service) checkIPThrottle(ctx context.Context, ip string) error {
	if s.opts.IPMaxAttempts <= 0 || ip == "" {
		return nil
	}
	n, _ := s.limiter.Count(ctx, ipKey(ip), s.opts.Lockout.Window)
	if n >= s.opts.IPMaxAttempts {
		return shared.NewRateLimited("too many login attempts", s.opts.Lockout.Window)
	}
	return nil
}

// unknownUserFailure mirrors the known-user path (bcrypt cost, counters,
// 423 after the threshold) so responses do not reveal whether an email exists.
func (s *Service) unknownUserFailure(ctx context.Context, email, password string, meta requestMeta) error {
	window := s.opts.Lockout.Window
	if n, _ := s.limiter.Count(ctx, emailKey(email), window); s.opts.Lockout.ShouldLock(n) {
		return lockedError(s.opts.Lockout.Base)
	}
	_ = bcrypt.CompareHashAndPassword(dummyHash(), []byte(password))
	if meta.IP != "" {
		_, _ = s.limiter.Hit(ctx, ipKey(meta.IP), window)
	}
	n, _ := s.limiter.Hit(ctx, emailKey(email), window)
	if s.opts.Lockout.ShouldLock(n) {
		return lockedError(s.opts.Lockout.Base)
	}
	return errInvalidCredentials
}

// registerFailure counts a failed factor in both the shared window and the
// users row, locking the account once the policy threshold is reached.
// It returns fail, or a locked error when this failure triggered a lock.
func (s *Service) registerFailure(ctx context.Context, user *identity.User, email string, meta requestMeta, reason string, fail error) error {
	now := s.now()
	window := s.opts.Lockout.Window
	if meta.IP != "" {
		_, _ = s.limiter.Hit(ctx, ipKey(meta.IP), window)
	}
	windowN, _ := s.limiter.Hit(ctx, emailKey(normalizeEmail(email)), window)
	dbN, lockouts, err := s.lockouts.RegisterLoginFailure(ctx, user.ID, now.Add(-window), now)
	if err != nil {
		return err
	}
	failures := max(dbN, windowN)
	s.record(ctx, user.ID, user, "auth.login_failed", meta, map[string]any{"reason": reason, "failures": failures})
	if !s.opts.Lockout.ShouldLock(failures) {
		return fail
	}

	d := s.opts.Lockout.LockDuration(lockouts)
	until := now.Add(d)
	if err := s.lockouts.LockUser(ctx, user.ID, until); err != nil {
		return err
	}
	_ = s.limiter.Reset(ctx, emailKey(normalizeEmail(email)))
	s.record(ctx, user.ID, user, "auth.account_locked", meta, map[string]any{
		"locked_until": until, "duration_seconds": int(d.Seconds()), "lockout_count": lockouts + 1,
	})
	return lockedError(d)
}

func (s *Service) clearFailures(ctx context.Context, userID uuid.UUID, email string) {
	_ = s.lockouts.ClearLoginFailures(ctx, userID)
	_ = s.limiter.Reset(ctx, emailKey(normalizeEmail(email)))
}

func (s *Service) recordFactorUse(ctx context.Context, user *identity.User, method string, meta requestMeta) {
	s.record(ctx, user.ID, user, "auth.mfa_verified", meta, map[string]any{"method": method})
	if method != methodRecovery {
		return
	}
	remaining, _ := s.mfa.CountRecoveryCodes(ctx, user.ID)
	id, branch := user.ID, user.BranchID
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: id, Action: "auth.mfa_recovery_used", EntityType: "user", EntityID: &id, BranchID: &branch,
		IP: meta.IP, UserAgent: meta.UserAgent, Extra: map[string]any{"remaining": remaining},
	})
}
