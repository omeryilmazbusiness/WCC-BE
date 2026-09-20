package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MFAProvider is an optional second-factor hook (T-012).
// Swap Noop/InMemory for TOTP/SMS later without changing Login flow (OCP).
type MFAProvider interface {
	IsEnabled(userID uuid.UUID, role Role, userFlag bool) bool
	StartChallenge(ctx context.Context, userID uuid.UUID) (challengeToken string, err error)
	Verify(ctx context.Context, challengeToken, code string) (userID uuid.UUID, err error)
}

// PolicyMFA enables MFA for gm/admin when the user flag is on (or forceRoles).
type PolicyMFA struct {
	ForceRoles map[Role]bool
	inner      *InMemoryMFA
}

func NewPolicyMFA(force ...Role) *PolicyMFA {
	m := map[Role]bool{}
	for _, r := range force {
		m[r] = true
	}
	return &PolicyMFA{ForceRoles: m, inner: NewInMemoryMFA()}
}

func (p *PolicyMFA) IsEnabled(userID uuid.UUID, role Role, userFlag bool) bool {
	if userFlag {
		return true
	}
	return p.ForceRoles[role]
}

func (p *PolicyMFA) StartChallenge(ctx context.Context, userID uuid.UUID) (string, error) {
	return p.inner.StartChallenge(ctx, userID)
}

func (p *PolicyMFA) Verify(ctx context.Context, challengeToken, code string) (uuid.UUID, error) {
	return p.inner.Verify(ctx, challengeToken, code)
}

// InMemoryMFA is a dev-safe challenge store (accepts code 000000).
type InMemoryMFA struct {
	mu   sync.Mutex
	chal map[string]chalEntry
}

type chalEntry struct {
	UserID uuid.UUID
	Expiry time.Time
}

func NewInMemoryMFA() *InMemoryMFA {
	return &InMemoryMFA{chal: make(map[string]chalEntry)}
}

func (m *InMemoryMFA) IsEnabled(uuid.UUID, Role, bool) bool { return false }

func (m *InMemoryMFA) StartChallenge(_ context.Context, userID uuid.UUID) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	m.mu.Lock()
	m.chal[token] = chalEntry{UserID: userID, Expiry: time.Now().UTC().Add(5 * time.Minute)}
	m.mu.Unlock()
	return token, nil
}

func (m *InMemoryMFA) Verify(_ context.Context, challengeToken, code string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.chal[challengeToken]
	if !ok || time.Now().UTC().After(e.Expiry) {
		return uuid.Nil, fmtErr("invalid or expired mfa challenge")
	}
	if code != "000000" {
		return uuid.Nil, fmtErr("invalid mfa code")
	}
	delete(m.chal, challengeToken)
	return e.UserID, nil
}

type mfaError string

func (e mfaError) Error() string { return string(e) }
func fmtErr(s string) error      { return mfaError(s) }
