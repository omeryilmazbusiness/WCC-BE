package authsec

import (
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RefreshToken is the server-side record of an issued refresh token.
// Only the SHA-256 of the raw token is stored.
type RefreshToken struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	FamilyID      uuid.UUID
	TokenHash     string
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	RevokedReason string
	ReplacedBy    *uuid.UUID
	CreatedIP     string
	UserAgent     string
	CreatedAt     time.Time
}

// RefreshTokenPrefix marks opaque refresh tokens so malformed input and
// leaked secrets are recognisable without a lookup.
const RefreshTokenPrefix = "wrt_"

// refreshTokenBytes is 256 bits of entropy.
const refreshTokenBytes = 32

// NewRefreshToken returns a fresh opaque refresh token.
func NewRefreshToken(random io.Reader) (string, error) {
	raw, err := NewOpaqueToken(random, refreshTokenBytes)
	if err != nil {
		return "", err
	}
	return RefreshTokenPrefix + raw, nil
}

// IsRefreshToken is a cheap syntactic check before any storage lookup.
func IsRefreshToken(s string) bool {
	return strings.HasPrefix(s, RefreshTokenPrefix) && len(s) == len(RefreshTokenPrefix)+43
}

// RefreshVerdict is the outcome of presenting a refresh token.
type RefreshVerdict int

const (
	RefreshRotate  RefreshVerdict = iota // valid: revoke and issue successor
	RefreshReused                        // already revoked: revoke the whole session
	RefreshExpired                       // past idle or absolute expiry: reject, end the session
	RefreshRevoked                       // session already ended: reject
	RefreshGrace                         // just rotated by a concurrent request: issue a sibling
)

// Revocation reasons stored on refresh_tokens.revoked_reason and
// auth_sessions.revoked_reason.
const (
	RevokeRotated         = "rotated"
	RevokeReuse           = "reuse_detected"
	RevokeLogout          = "logout"
	RevokeUser            = "user_revoked"
	RevokeAdmin           = "admin"
	RevokeExpired         = "expired"
	RevokeMFADisabled     = "mfa_disabled"
	RevokeUserDeactivated = "user_deactivated"
	RevokePasswordChanged = "password_changed"
)

// EvaluateRefresh decides what to do with a presented token of session s.
// Reuse wins over expiry so a stolen, rotated token always burns its session,
// except inside the grace window after a rotation, where a second request
// racing the first is expected (parallel tabs, retried requests).
func EvaluateRefresh(t RefreshToken, s Session, now time.Time, grace time.Duration) RefreshVerdict {
	if t.RevokedAt != nil {
		if t.RevokedReason == RevokeRotated && grace > 0 && now.Sub(*t.RevokedAt) < grace && s.Active(now) {
			return RefreshGrace
		}
		return RefreshReused
	}
	if s.RevokedAt != nil {
		return RefreshRevoked
	}
	if !t.ExpiresAt.After(now) || !now.Before(s.AbsoluteExpiresAt) || !now.Before(s.IdleExpiresAt) {
		return RefreshExpired
	}
	return RefreshRotate
}
