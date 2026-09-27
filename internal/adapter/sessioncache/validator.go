// Package sessioncache checks access tokens against their server-side session
// with a short per-instance cache.
//
// Revocations made on this instance invalidate the cache immediately; other
// instances converge within the TTL, which is therefore the worst-case
// revocation latency for access tokens.
package sessioncache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	appauth "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
)

// DefaultMaxEntries bounds memory at roughly one entry per active session.
const DefaultMaxEntries = 10_000

// Store loads session state; (nil, nil) means the session does not exist.
type Store interface {
	SessionState(ctx context.Context, sid uuid.UUID) (*authsec.SessionState, error)
}

type entry struct {
	state   *authsec.SessionState
	expires time.Time
}

// Validator caches session state (not verdicts), so expiry is still judged
// against the current time on every request.
type Validator struct {
	store Store
	ttl   time.Duration
	max   int
	now   func() time.Time

	mu      sync.Mutex
	entries map[uuid.UUID]entry
	// gen changes on every invalidation so a load that raced one is not cached.
	gen uint64
}

// New returns a validator; ttl <= 0 disables caching.
func New(store Store, ttl time.Duration, maxEntries int) *Validator {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxEntries
	}
	return &Validator{
		store: store, ttl: ttl, max: maxEntries,
		now:     func() time.Time { return time.Now().UTC() },
		entries: map[uuid.UUID]entry{},
	}
}

// Validate returns authsec.ErrSessionRevoked or authsec.ErrTokenStale for a
// rejected token, and any other error when the check itself failed; callers
// must fail closed on the latter.
func (v *Validator) Validate(ctx context.Context, sid, userID uuid.UUID, version int) error {
	st, err := v.load(ctx, sid)
	if err != nil {
		return fmt.Errorf("session check: %w", err)
	}
	return authsec.CheckSession(st, userID, version, v.now())
}

func (v *Validator) Invalidate(sid uuid.UUID) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.gen++
	delete(v.entries, sid)
}

func (v *Validator) InvalidateUser(userID uuid.UUID) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.gen++
	for sid, e := range v.entries {
		if e.state != nil && e.state.UserID == userID {
			delete(v.entries, sid)
		}
	}
}

func (v *Validator) load(ctx context.Context, sid uuid.UUID) (*authsec.SessionState, error) {
	if v.ttl <= 0 {
		return v.store.SessionState(ctx, sid)
	}
	v.mu.Lock()
	e, ok := v.entries[sid]
	gen := v.gen
	v.mu.Unlock()
	if ok && v.now().Before(e.expires) {
		return e.state, nil
	}

	st, err := v.store.SessionState(ctx, sid)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.gen == gen {
		v.put(sid, entry{state: st, expires: v.now().Add(v.ttl)})
	}
	return st, nil
}

// put must hold mu.
func (v *Validator) put(sid uuid.UUID, e entry) {
	if _, exists := v.entries[sid]; !exists && len(v.entries) >= v.max {
		now := v.now()
		for k, old := range v.entries {
			if !now.Before(old.expires) {
				delete(v.entries, k)
			}
		}
		for k := range v.entries {
			if len(v.entries) < v.max {
				break
			}
			delete(v.entries, k)
		}
	}
	v.entries[sid] = e
}

var _ appauth.SessionInvalidator = (*Validator)(nil)
