package authsec

import (
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLockDurationDoublesAndCaps(t *testing.T) {
	p := LockoutPolicy{MaxAttempts: 5, Window: 15 * time.Minute, Base: 15 * time.Minute, Max: 24 * time.Hour}
	want := []time.Duration{
		15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour, 4 * time.Hour,
		8 * time.Hour, 16 * time.Hour, 24 * time.Hour, 24 * time.Hour,
	}
	for i, w := range want {
		if got := p.LockDuration(i); got != w {
			t.Fatalf("lockouts=%d: got %s want %s", i, got, w)
		}
	}
	if got := p.LockDuration(1000); got != 24*time.Hour {
		t.Fatalf("large count must cap, got %s", got)
	}
}

func TestShouldLock(t *testing.T) {
	p := LockoutPolicy{MaxAttempts: 5}
	if p.ShouldLock(4) || !p.ShouldLock(5) || !p.ShouldLock(6) {
		t.Fatal("threshold mismatch")
	}
	if (LockoutPolicy{}).ShouldLock(100) {
		t.Fatal("zero policy must never lock")
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Now()
	if RetryAfter(nil, now) != 0 {
		t.Fatal("nil lock")
	}
	past := now.Add(-time.Second)
	if RetryAfter(&past, now) != 0 {
		t.Fatal("expired lock")
	}
	fut := now.Add(1500 * time.Millisecond)
	if got := RetryAfterSeconds(RetryAfter(&fut, now)); got != 2 {
		t.Fatalf("round up: got %d", got)
	}
}

func TestEvaluateRefresh(t *testing.T) {
	now := time.Now()
	ago := func(d time.Duration) *time.Time { v := now.Add(-d); return &v }
	sess := Session{AbsoluteExpiresAt: now.Add(24 * time.Hour), IdleExpiresAt: now.Add(time.Hour)}
	live := RefreshToken{ExpiresAt: now.Add(time.Hour)}
	expired := RefreshToken{ExpiresAt: now.Add(-time.Second)}
	rotated := func(d time.Duration) RefreshToken {
		t := live
		t.RevokedAt, t.RevokedReason = ago(d), RevokeRotated
		return t
	}
	revokedSess := sess
	revokedSess.RevokedAt = ago(time.Second)
	pastAbsolute := sess
	pastAbsolute.AbsoluteExpiresAt = now.Add(-time.Second)
	grace := 10 * time.Second

	cases := []struct {
		name  string
		tok   RefreshToken
		sess  Session
		grace time.Duration
		want  RefreshVerdict
	}{
		{"live token rotates", live, sess, grace, RefreshRotate},
		{"expired token", expired, sess, grace, RefreshExpired},
		{"past absolute lifetime", live, pastAbsolute, grace, RefreshExpired},
		{"session revoked, token not", live, revokedSess, grace, RefreshRevoked},
		{"rotated inside grace", rotated(2 * time.Second), sess, grace, RefreshGrace},
		{"rotated at grace boundary", rotated(grace), sess, grace, RefreshReused},
		{"rotated outside grace", rotated(time.Minute), sess, grace, RefreshReused},
		{"grace disabled", rotated(time.Second), sess, 0, RefreshReused},
		{"grace needs an active session", rotated(time.Second), revokedSess, grace, RefreshReused},
		{"grace needs an unexpired session", rotated(time.Second), pastAbsolute, grace, RefreshReused},
		{"revoked for another reason", func() RefreshToken { t := rotated(time.Second); t.RevokedReason = RevokeLogout; return t }(), sess, grace, RefreshReused},
		{"reuse wins over expiry", func() RefreshToken { t := expired; t.RevokedAt = ago(time.Hour); return t }(), sess, grace, RefreshReused},
	}
	for _, c := range cases {
		if got := EvaluateRefresh(c.tok, c.sess, now, c.grace); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}

func TestSuccessorExpiry(t *testing.T) {
	now := time.Now()
	idle := 7 * 24 * time.Hour
	far := now.Add(30 * 24 * time.Hour)
	if got := SuccessorExpiry(now, idle, far); !got.Equal(now.Add(idle)) {
		t.Fatalf("idle window must apply when absolute is far: %s", got)
	}
	near := now.Add(time.Hour)
	if got := SuccessorExpiry(now, idle, near); !got.Equal(near) {
		t.Fatalf("absolute expiry must cap the idle window: %s", got)
	}
	idleAt, absAt := SessionPolicy{Idle: idle, Absolute: 30 * 24 * time.Hour}.Start(now)
	if !absAt.Equal(far) || !idleAt.Equal(now.Add(idle)) {
		t.Fatalf("start: idle=%s abs=%s", idleAt, absAt)
	}
	idleAt, absAt = SessionPolicy{Idle: idle, Absolute: time.Hour}.Start(now)
	if !idleAt.Equal(absAt) {
		t.Fatal("idle must never outlive absolute")
	}
}

func TestCheckSession(t *testing.T) {
	now := time.Now()
	uid := uuid.New()
	base := SessionState{UserID: uid, AbsoluteExpiresAt: now.Add(time.Hour), IdleExpiresAt: now.Add(time.Hour), UserActive: true, TokenVersion: 3}
	with := func(f func(*SessionState)) *SessionState { s := base; f(&s); return &s }
	cases := []struct {
		name string
		st   *SessionState
		uid  uuid.UUID
		ver  int
		want error
	}{
		{"valid", &base, uid, 3, nil},
		{"missing session", nil, uid, 3, ErrSessionRevoked},
		{"other user", &base, uuid.New(), 3, ErrSessionRevoked},
		{"revoked", with(func(s *SessionState) { s.RevokedAt = &now }), uid, 3, ErrSessionRevoked},
		{"inactive user", with(func(s *SessionState) { s.UserActive = false }), uid, 3, ErrSessionRevoked},
		{"past absolute", with(func(s *SessionState) { s.AbsoluteExpiresAt = now }), uid, 3, ErrSessionRevoked},
		{"past idle", with(func(s *SessionState) { s.IdleExpiresAt = now.Add(-time.Second) }), uid, 3, ErrSessionRevoked},
		{"stale version", &base, uid, 2, ErrTokenStale},
		{"revocation wins over staleness", with(func(s *SessionState) { s.UserActive = false }), uid, 2, ErrSessionRevoked},
	}
	for _, c := range cases {
		if got := CheckSession(c.st, c.uid, c.ver, now); !errors.Is(got, c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestRefreshTokenFormat(t *testing.T) {
	a, err := NewRefreshToken(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewRefreshToken(rand.Reader)
	if a == b || !strings.HasPrefix(a, RefreshTokenPrefix) || !IsRefreshToken(a) {
		t.Fatalf("bad token %q", a)
	}
	for _, bad := range []string{"", "wrt_", "eyJhbGciOiJIUzI1NiJ9.x.y", a[4:], a + "x"} {
		if IsRefreshToken(bad) {
			t.Errorf("%q must not look like a refresh token", bad)
		}
	}
}

func TestRecoveryCodes(t *testing.T) {
	codes, hashes, err := NewRecoveryCodes(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCodeCount || len(hashes) != RecoveryCodeCount {
		t.Fatalf("count %d/%d", len(codes), len(hashes))
	}
	seen := map[string]bool{}
	for i, c := range codes {
		if len(c) != 19 || strings.Count(c, "-") != 3 {
			t.Fatalf("format %q", c)
		}
		if seen[c] {
			t.Fatal("duplicate code")
		}
		seen[c] = true
		if HashRecoveryCode(strings.ToUpper(strings.ReplaceAll(c, "-", " "))) != hashes[i] {
			t.Fatal("hash must be normalization-insensitive")
		}
	}
}

func TestOpaqueToken(t *testing.T) {
	a, _ := NewOpaqueToken(rand.Reader, 32)
	b, _ := NewOpaqueToken(rand.Reader, 32)
	if a == b || len(a) < 40 || HashToken(a) == HashToken(b) {
		t.Fatal("tokens must be random")
	}
}
