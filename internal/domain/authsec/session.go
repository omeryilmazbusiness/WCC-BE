package authsec

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Session is one signed-in device: the refresh-token family and the unit of
// revocation for access tokens (their sid claim).
type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	CreatedAt         time.Time
	AbsoluteExpiresAt time.Time
	IdleExpiresAt     time.Time
	LastSeenAt        time.Time
	LastIP            string
	UserAgent         string
	AuthMethod        string
	RevokedAt         *time.Time
	RevokedReason     string
}

// Values of auth_sessions.auth_method.
const (
	AuthMethodPassword = "password"
	AuthMethodTOTP     = "totp"
	AuthMethodRecovery = "recovery"
	AuthMethodMFASetup = "mfa_setup"
)

// Active reports whether the session may still authenticate requests.
func (s Session) Active(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.AbsoluteExpiresAt) && now.Before(s.IdleExpiresAt)
}

// SessionPolicy bounds a session by inactivity (idle, renewed on every
// refresh) and by age (absolute, never renewed).
type SessionPolicy struct {
	Idle     time.Duration
	Absolute time.Duration
}

// Start returns the expiries of a session created at now.
func (p SessionPolicy) Start(now time.Time) (idle, absolute time.Time) {
	absolute = now.Add(p.Absolute)
	return SuccessorExpiry(now, p.Idle, absolute), absolute
}

// SuccessorExpiry is the expiry of a refresh token issued at now: the idle
// window, capped by the session's absolute expiry.
func SuccessorExpiry(now time.Time, idle time.Duration, absolute time.Time) time.Time {
	if next := now.Add(idle); next.Before(absolute) {
		return next
	}
	return absolute
}

// SessionState is what an access-token check needs to know about its session.
type SessionState struct {
	SessionID         uuid.UUID
	UserID            uuid.UUID
	RevokedAt         *time.Time
	AbsoluteExpiresAt time.Time
	IdleExpiresAt     time.Time
	UserActive        bool
	TokenVersion      int
}

var (
	// ErrSessionRevoked: the session is revoked, expired or its user inactive; sign in again.
	ErrSessionRevoked = errors.New("session revoked")
	// ErrTokenStale: the user's claims changed since the token was issued; refresh it.
	ErrTokenStale = errors.New("token stale")
)

// CheckSession validates an access token's sid/uid/ver against stored state.
// A nil state means the session does not exist.
func CheckSession(st *SessionState, userID uuid.UUID, version int, now time.Time) error {
	if st == nil || st.UserID != userID || st.RevokedAt != nil || !st.UserActive ||
		!now.Before(st.AbsoluteExpiresAt) || !now.Before(st.IdleExpiresAt) {
		return ErrSessionRevoked
	}
	if st.TokenVersion != version {
		return ErrTokenStale
	}
	return nil
}
