package auth

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// SecurityState is the per-user MFA and lockout state kept beside identity.User.
type SecurityState struct {
	MFAEnabled    bool
	MFASecretEnc  string
	MFAPendingEnc string
	MFALastStep   int64
	FailedLogins  int
	Lockouts      int
	LockedUntil   *time.Time
}

// LockoutStore persists failed-login counters and locks so they survive
// rate-limit store loss.
type LockoutStore interface {
	SecurityState(ctx context.Context, userID uuid.UUID) (*SecurityState, error)
	// RegisterLoginFailure counts a failure, restarting the count when the
	// previous failure is older than windowStart.
	RegisterLoginFailure(ctx context.Context, userID uuid.UUID, windowStart, now time.Time) (failures, lockouts int, err error)
	// LockUser sets locked_until, bumps lockout_count and clears the failure count.
	LockUser(ctx context.Context, userID uuid.UUID, until time.Time) error
	// ClearLoginFailures resets counters and consecutive lockouts after a success.
	ClearLoginFailures(ctx context.Context, userID uuid.UUID) error
	UnlockUser(ctx context.Context, userID uuid.UUID) error
}

type MFAStore interface {
	SetPendingMFASecret(ctx context.Context, userID uuid.UUID, enc string) error
	ActivateMFA(ctx context.Context, userID uuid.UUID, secretEnc string, step int64, now time.Time) error
	DeactivateMFA(ctx context.Context, userID uuid.UUID) error
	// AdvanceMFAStep stores step only if it is newer than the stored one;
	// false means the code was already used.
	AdvanceMFAStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error)
	ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes []string) error
	ConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, hash string, now time.Time) (bool, error)
	CountRecoveryCodes(ctx context.Context, userID uuid.UUID) (int, error)
}

type ChallengeStore interface {
	CreateChallenge(ctx context.Context, c authsec.Challenge) error
	FindChallenge(ctx context.Context, tokenHash, purpose string, now time.Time) (*authsec.Challenge, error)
	// AttemptChallenge atomically counts one attempt on a live challenge and
	// returns nil when it is unknown, expired or out of attempts.
	AttemptChallenge(ctx context.Context, tokenHash, purpose string, maxAttempts int, now time.Time) (*authsec.Challenge, error)
	DeleteChallenge(ctx context.Context, tokenHash string) error
}

type RefreshStore interface {
	CreateRefreshToken(ctx context.Context, t *authsec.RefreshToken) error
	FindRefreshToken(ctx context.Context, tokenHash string) (*authsec.RefreshToken, error)
	// MarkRefreshRotated revokes id only if still active; false means a
	// concurrent or repeated use already consumed it.
	MarkRefreshRotated(ctx context.Context, id, replacedBy uuid.UUID, now time.Time) (bool, error)
}

// SessionStore persists sessions. Revoking a session revokes its refresh
// tokens in the same statement.
type SessionStore interface {
	CreateSession(ctx context.Context, s *authsec.Session) error
	FindSession(ctx context.Context, id uuid.UUID) (*authsec.Session, error)
	// TouchSession extends a live session after a refresh; false means it was
	// revoked meanwhile.
	TouchSession(ctx context.Context, id uuid.UUID, idleExpiresAt, seenAt time.Time, ip string) (bool, error)
	// ListActiveSessions returns unrevoked, unexpired sessions, newest first.
	ListActiveSessions(ctx context.Context, userID uuid.UUID, now time.Time) ([]authsec.Session, error)
	// RevokeSession reports false when the session was already revoked.
	RevokeSession(ctx context.Context, id uuid.UUID, reason string, now time.Time) (bool, error)
	// RevokeUserSessions revokes every live session of userID except keep
	// (uuid.Nil keeps none) and returns the revoked ids.
	RevokeUserSessions(ctx context.Context, userID, keep uuid.UUID, reason string, now time.Time) ([]uuid.UUID, error)
}

// SessionInvalidator drops this instance's cached access-token checks after a
// revocation. Other instances converge within the cache TTL.
type SessionInvalidator interface {
	Invalidate(sessionID uuid.UUID)
	InvalidateUser(userID uuid.UUID)
}

type AccessTokenIssuer interface {
	IssueAccess(sub platformauth.AccessSubject) (platformauth.AccessToken, error)
}

type noopInvalidator struct{}

func (noopInvalidator) Invalidate(uuid.UUID)     {}
func (noopInvalidator) InvalidateUser(uuid.UUID) {}
