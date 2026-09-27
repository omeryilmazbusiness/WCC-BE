package sessioncache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
)

type fakeStore struct {
	states map[uuid.UUID]authsec.SessionState
	calls  int
	err    error
}

func (f *fakeStore) SessionState(_ context.Context, sid uuid.UUID) (*authsec.SessionState, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	st, ok := f.states[sid]
	if !ok {
		return nil, nil
	}
	return &st, nil
}

type fixture struct {
	v     *Validator
	store *fakeStore
	clock time.Time
	sid   uuid.UUID
	uid   uuid.UUID
}

func newFixture(ttl time.Duration, max int) *fixture {
	f := &fixture{clock: time.Now(), sid: uuid.New(), uid: uuid.New()}
	f.store = &fakeStore{states: map[uuid.UUID]authsec.SessionState{
		f.sid: {SessionID: f.sid, UserID: f.uid, AbsoluteExpiresAt: f.clock.Add(time.Hour), IdleExpiresAt: f.clock.Add(time.Hour), UserActive: true, TokenVersion: 1},
	}}
	f.v = New(f.store, ttl, max)
	f.v.now = func() time.Time { return f.clock }
	return f
}

func (f *fixture) revoke() {
	st := f.store.states[f.sid]
	now := f.clock
	st.RevokedAt = &now
	f.store.states[f.sid] = st
}

func TestCacheHitAvoidsStore(t *testing.T) {
	f := newFixture(10*time.Second, 0)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := f.v.Validate(ctx, f.sid, f.uid, 1); err != nil {
			t.Fatal(err)
		}
	}
	if f.store.calls != 1 {
		t.Fatalf("want 1 store call, got %d", f.store.calls)
	}
	f.revoke()
	if err := f.v.Validate(ctx, f.sid, f.uid, 1); err != nil {
		t.Fatalf("remote revocation is invisible until the TTL passes: %v", err)
	}
	f.clock = f.clock.Add(11 * time.Second)
	if err := f.v.Validate(ctx, f.sid, f.uid, 1); !errors.Is(err, authsec.ErrSessionRevoked) {
		t.Fatalf("after TTL the revocation must be seen: %v", err)
	}
}

func TestInvalidateDropsEntry(t *testing.T) {
	f := newFixture(time.Minute, 0)
	ctx := context.Background()
	_ = f.v.Validate(ctx, f.sid, f.uid, 1)
	f.revoke()
	f.v.Invalidate(f.sid)
	if err := f.v.Validate(ctx, f.sid, f.uid, 1); !errors.Is(err, authsec.ErrSessionRevoked) {
		t.Fatalf("local revocation must apply immediately: %v", err)
	}
}

func TestInvalidateUserDropsAllSessions(t *testing.T) {
	f := newFixture(time.Minute, 0)
	ctx := context.Background()
	other := uuid.New()
	f.store.states[other] = authsec.SessionState{SessionID: other, UserID: uuid.New(), AbsoluteExpiresAt: f.clock.Add(time.Hour), IdleExpiresAt: f.clock.Add(time.Hour), UserActive: true, TokenVersion: 1}
	_ = f.v.Validate(ctx, f.sid, f.uid, 1)
	_ = f.v.Validate(ctx, other, f.store.states[other].UserID, 1)

	st := f.store.states[f.sid]
	st.TokenVersion = 2
	f.store.states[f.sid] = st
	f.v.InvalidateUser(f.uid)
	if err := f.v.Validate(ctx, f.sid, f.uid, 1); !errors.Is(err, authsec.ErrTokenStale) {
		t.Fatalf("version bump must surface as token_stale: %v", err)
	}
	calls := f.store.calls
	_ = f.v.Validate(ctx, other, f.store.states[other].UserID, 1)
	if f.store.calls != calls {
		t.Fatal("other users' entries must survive")
	}
}

func TestStoreErrorFailsClosedAndIsNotCached(t *testing.T) {
	f := newFixture(time.Minute, 0)
	f.store.err = errors.New("db down")
	err := f.v.Validate(context.Background(), f.sid, f.uid, 1)
	if err == nil || errors.Is(err, authsec.ErrSessionRevoked) || errors.Is(err, authsec.ErrTokenStale) {
		t.Fatalf("store failure must be a distinct error (fail closed): %v", err)
	}
	f.store.err = nil
	if err := f.v.Validate(context.Background(), f.sid, f.uid, 1); err != nil {
		t.Fatalf("errors must not be cached: %v", err)
	}
}

func TestExpiryJudgedAtRequestTime(t *testing.T) {
	f := newFixture(time.Minute, 0)
	st := f.store.states[f.sid]
	st.IdleExpiresAt = f.clock.Add(5 * time.Second)
	f.store.states[f.sid] = st
	_ = f.v.Validate(context.Background(), f.sid, f.uid, 1)
	f.clock = f.clock.Add(6 * time.Second)
	if err := f.v.Validate(context.Background(), f.sid, f.uid, 1); !errors.Is(err, authsec.ErrSessionRevoked) {
		t.Fatalf("cached state must still expire on time: %v", err)
	}
}

func TestUnknownSessionAndDisabledCache(t *testing.T) {
	f := newFixture(0, 0)
	ctx := context.Background()
	if err := f.v.Validate(ctx, uuid.New(), f.uid, 1); !errors.Is(err, authsec.ErrSessionRevoked) {
		t.Fatalf("unknown sid: %v", err)
	}
	_ = f.v.Validate(ctx, f.sid, f.uid, 1)
	_ = f.v.Validate(ctx, f.sid, f.uid, 1)
	if f.store.calls != 3 {
		t.Fatalf("ttl=0 must always hit the store, got %d calls", f.store.calls)
	}
}

func TestCacheIsBounded(t *testing.T) {
	f := newFixture(time.Minute, 3)
	for i := 0; i < 10; i++ {
		_ = f.v.Validate(context.Background(), uuid.New(), f.uid, 1)
	}
	if n := len(f.v.entries); n > 3 {
		t.Fatalf("cache grew to %d entries", n)
	}
}
