package auth

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/totp"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
)

type fakeUsers struct {
	identity.Repository
	byID map[uuid.UUID]*identity.User
	// onUpdate stands in for the users triggers (token_version bump, session revocation).
	onUpdate func(before, after *identity.User)
}

func (f *fakeUsers) UpdateUser(_ context.Context, u *identity.User) error {
	before, ok := f.byID[u.ID]
	if !ok {
		return errors.New("user: not found")
	}
	cp := *u
	if f.onUpdate != nil {
		f.onUpdate(before, &cp)
	}
	f.byID[u.ID] = &cp
	return nil
}

func (f *fakeUsers) FindUserByEmail(_ context.Context, email string) (*identity.User, error) {
	for _, u := range f.byID {
		if u.Email == email {
			cp := *u
			return &cp, nil
		}
	}
	return nil, errors.New("user: not found")
}

func (f *fakeUsers) FindUserByID(_ context.Context, id uuid.UUID) (*identity.User, error) {
	if u, ok := f.byID[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, errors.New("user: not found")
}

type fakeSec struct {
	state      map[uuid.UUID]*SecurityState
	lastFail   map[uuid.UUID]time.Time
	recovery   map[uuid.UUID]map[string]bool
	challenges map[string]*authsec.Challenge
	refresh    map[string]*authsec.RefreshToken
	sessions   map[uuid.UUID]*authsec.Session
	// beforeRotate runs once inside the next MarkRefreshRotated (race simulation).
	beforeRotate func()
}

func newFakeSec() *fakeSec {
	return &fakeSec{
		state: map[uuid.UUID]*SecurityState{}, lastFail: map[uuid.UUID]time.Time{},
		recovery: map[uuid.UUID]map[string]bool{}, challenges: map[string]*authsec.Challenge{},
		refresh: map[string]*authsec.RefreshToken{}, sessions: map[uuid.UUID]*authsec.Session{},
	}
}

func (f *fakeSec) SecurityState(_ context.Context, id uuid.UUID) (*SecurityState, error) {
	cp := *f.state[id]
	return &cp, nil
}

func (f *fakeSec) RegisterLoginFailure(_ context.Context, id uuid.UUID, windowStart, now time.Time) (int, int, error) {
	st := f.state[id]
	if last, ok := f.lastFail[id]; !ok || last.Before(windowStart) {
		st.FailedLogins = 1
	} else {
		st.FailedLogins++
	}
	f.lastFail[id] = now
	return st.FailedLogins, st.Lockouts, nil
}

func (f *fakeSec) LockUser(_ context.Context, id uuid.UUID, until time.Time) error {
	st := f.state[id]
	st.LockedUntil, st.Lockouts, st.FailedLogins = &until, st.Lockouts+1, 0
	return nil
}

func (f *fakeSec) ClearLoginFailures(_ context.Context, id uuid.UUID) error {
	st := f.state[id]
	st.FailedLogins, st.Lockouts, st.LockedUntil = 0, 0, nil
	delete(f.lastFail, id)
	return nil
}

func (f *fakeSec) UnlockUser(ctx context.Context, id uuid.UUID) error {
	return f.ClearLoginFailures(ctx, id)
}

func (f *fakeSec) SetPendingMFASecret(_ context.Context, id uuid.UUID, enc string) error {
	f.state[id].MFAPendingEnc = enc
	return nil
}

func (f *fakeSec) ActivateMFA(_ context.Context, id uuid.UUID, enc string, step int64, _ time.Time) error {
	st := f.state[id]
	st.MFAEnabled, st.MFASecretEnc, st.MFAPendingEnc, st.MFALastStep = true, enc, "", step
	return nil
}

func (f *fakeSec) DeactivateMFA(_ context.Context, id uuid.UUID) error {
	st := f.state[id]
	st.MFAEnabled, st.MFASecretEnc, st.MFAPendingEnc, st.MFALastStep = false, "", "", 0
	delete(f.recovery, id)
	return nil
}

func (f *fakeSec) AdvanceMFAStep(_ context.Context, id uuid.UUID, step int64) (bool, error) {
	st := f.state[id]
	if st.MFALastStep >= step {
		return false, nil
	}
	st.MFALastStep = step
	return true, nil
}

func (f *fakeSec) ReplaceRecoveryCodes(_ context.Context, id uuid.UUID, hashes []string) error {
	m := map[string]bool{}
	for _, h := range hashes {
		m[h] = true
	}
	f.recovery[id] = m
	return nil
}

func (f *fakeSec) ConsumeRecoveryCode(_ context.Context, id uuid.UUID, hash string, _ time.Time) (bool, error) {
	if f.recovery[id][hash] {
		delete(f.recovery[id], hash)
		return true, nil
	}
	return false, nil
}

func (f *fakeSec) CountRecoveryCodes(_ context.Context, id uuid.UUID) (int, error) {
	return len(f.recovery[id]), nil
}

func (f *fakeSec) CreateChallenge(_ context.Context, c authsec.Challenge) error {
	f.challenges[c.TokenHash] = &c
	return nil
}

func (f *fakeSec) FindChallenge(_ context.Context, hash, purpose string, now time.Time) (*authsec.Challenge, error) {
	c := f.challenges[hash]
	if c == nil || c.Purpose != purpose || !c.ExpiresAt.After(now) {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}

func (f *fakeSec) AttemptChallenge(ctx context.Context, hash, purpose string, max int, now time.Time) (*authsec.Challenge, error) {
	c, _ := f.FindChallenge(ctx, hash, purpose, now)
	if c == nil || c.Attempts >= max {
		return nil, nil
	}
	f.challenges[hash].Attempts++
	c.Attempts++
	return c, nil
}

func (f *fakeSec) DeleteChallenge(_ context.Context, hash string) error {
	delete(f.challenges, hash)
	return nil
}

func (f *fakeSec) CreateRefreshToken(_ context.Context, t *authsec.RefreshToken) error {
	cp := *t
	f.refresh[t.TokenHash] = &cp
	return nil
}

func (f *fakeSec) FindRefreshToken(_ context.Context, hash string) (*authsec.RefreshToken, error) {
	if t := f.refresh[hash]; t != nil {
		cp := *t
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeSec) MarkRefreshRotated(_ context.Context, id, next uuid.UUID, now time.Time) (bool, error) {
	if hook := f.beforeRotate; hook != nil {
		f.beforeRotate = nil
		hook()
	}
	for _, t := range f.refresh {
		if t.ID == id && t.RevokedAt == nil {
			t.RevokedAt, t.RevokedReason, t.ReplacedBy = &now, authsec.RevokeRotated, &next
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeSec) CreateSession(_ context.Context, s *authsec.Session) error {
	cp := *s
	f.sessions[s.ID] = &cp
	return nil
}

func (f *fakeSec) FindSession(_ context.Context, id uuid.UUID) (*authsec.Session, error) {
	if s := f.sessions[id]; s != nil {
		cp := *s
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeSec) TouchSession(_ context.Context, id uuid.UUID, idle, seen time.Time, ip string) (bool, error) {
	s := f.sessions[id]
	if s == nil || s.RevokedAt != nil {
		return false, nil
	}
	s.IdleExpiresAt, s.LastSeenAt, s.LastIP = idle, seen, ip
	return true, nil
}

func (f *fakeSec) ListActiveSessions(_ context.Context, user uuid.UUID, now time.Time) ([]authsec.Session, error) {
	out := []authsec.Session{}
	for _, s := range f.sessions {
		if s.UserID == user && s.Active(now) {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeSec) RevokeSession(_ context.Context, id uuid.UUID, reason string, now time.Time) (bool, error) {
	for _, t := range f.refresh {
		if t.FamilyID == id && t.RevokedAt == nil {
			t.RevokedAt, t.RevokedReason = &now, reason
		}
	}
	s := f.sessions[id]
	if s == nil || s.RevokedAt != nil {
		return false, nil
	}
	s.RevokedAt, s.RevokedReason = &now, reason
	return true, nil
}

func (f *fakeSec) RevokeUserSessions(ctx context.Context, user, keep uuid.UUID, reason string, now time.Time) ([]uuid.UUID, error) {
	ids := []uuid.UUID{}
	for id, s := range f.sessions {
		if s.UserID == user && id != keep && s.Active(now) {
			_, _ = f.RevokeSession(ctx, id, reason, now)
			ids = append(ids, id)
		}
	}
	return ids, nil
}

type fakeCache struct {
	sessions []uuid.UUID
	users    []uuid.UUID
}

func (f *fakeCache) Invalidate(id uuid.UUID)     { f.sessions = append(f.sessions, id) }
func (f *fakeCache) InvalidateUser(id uuid.UUID) { f.users = append(f.users, id) }

func (f *fakeCache) sawSession(id uuid.UUID) bool {
	for _, s := range f.sessions {
		if s == id {
			return true
		}
	}
	return false
}

type fakeAudit struct{ actions []string }

func (f *fakeAudit) Record(_ context.Context, in audit.RecordInput) error {
	f.actions = append(f.actions, in.Action)
	return nil
}

func (f *fakeAudit) has(action string) bool {
	for _, a := range f.actions {
		if a == action {
			return true
		}
	}
	return false
}

type harness struct {
	svc    *Service
	sec    *fakeSec
	audit  *fakeAudit
	cache  *fakeCache
	tokens *platformauth.TokenService
	users  *fakeUsers
	user   *identity.User
	clock  time.Time
}

const (
	testIdle     = time.Hour
	testAbsolute = 3 * time.Hour
	testGrace    = 10 * time.Second
)

func newHarness(t *testing.T, role platformauth.Role, forced ...platformauth.Role) *harness {
	t.Helper()
	hash, _ := bcrypt.GenerateFromPassword([]byte("correct-horse"), bcrypt.MinCost)
	u := &identity.User{
		ID: uuid.New(), Email: "user@wodi.test", PasswordHash: string(hash), Role: role,
		BranchID: uuid.New(), IsActive: true,
	}
	sec := newFakeSec()
	sec.state[u.ID] = &SecurityState{}
	keyring, err := crypto.NewKeyring("k1", "ZGV2LW9ubHktZW5jcnlwdGlvbi1rZXktMzJieXRlcyE=", nil)
	if err != nil {
		t.Fatal(err)
	}
	au := &fakeAudit{}
	tokens, err := platformauth.NewTokenServiceFromConfig(config.AuthConfig{
		JWTAccessSecret: "access-secret-for-tests-only-0000", JWTAccessKeyID: "t1",
		AccessTTL: 15 * time.Minute, Issuer: "test", Audience: "test-api",
	})
	if err != nil {
		t.Fatal(err)
	}
	// TokenService stamps expiry with the wall clock, so the fake clock starts there.
	h := &harness{
		sec: sec, audit: au, cache: &fakeCache{}, tokens: tokens, user: u, clock: time.Now().UTC(),
		users: &fakeUsers{byID: map[uuid.UUID]*identity.User{u.ID: u}},
	}
	h.svc = NewService(Deps{
		Users: h.users, Audit: au, Tokens: tokens,
		Lockouts: sec, MFA: sec, Challenges: sec, Refresh: sec, Sessions: sec, SessionCache: h.cache, Cipher: keyring,
		Options: Options{
			Lockout:       authsec.LockoutPolicy{MaxAttempts: 5, Window: 15 * time.Minute, Base: 15 * time.Minute, Max: 24 * time.Hour},
			IPMaxAttempts: 100,
			MFA:           platformauth.NewMFAPolicy(forced...),
			Session:       authsec.SessionPolicy{Idle: testIdle, Absolute: testAbsolute},
			ReuseGrace:    testGrace,
		},
	})
	h.svc.now = func() time.Time { return h.clock }
	return h
}

func (h *harness) login(password string) (*LoginResult, error) {
	return h.svc.Login(context.Background(), LoginInput{
		Email: "USER@wodi.test ", Password: password, Platform: h.user.Role == platformauth.RoleAdmin, IP: "10.0.0.1",
	})
}

func TestLoginLockoutAndUnlock(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	for i := 1; i < 5; i++ {
		if _, err := h.login("wrong"); !errors.Is(err, shared.ErrUnauthorized) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	_, err := h.login("wrong")
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(err, shared.ErrLocked) || app.RetryAfter != 15*time.Minute {
		t.Fatalf("5th failure must lock for 15m, got %v", err)
	}
	if _, err := h.login("correct-horse"); !errors.Is(err, shared.ErrLocked) {
		t.Fatalf("correct password while locked must stay locked: %v", err)
	}
	if !h.audit.has("auth.account_locked") {
		t.Fatal("lock must be audited")
	}

	if err := h.svc.Unlock(context.Background(), UnlockInput{ActorID: uuid.New(), UserID: h.user.ID}); err != nil {
		t.Fatal(err)
	}
	res, err := h.login("correct-horse")
	if err != nil || res.Tokens == nil {
		t.Fatalf("after unlock: %v", err)
	}
	if !h.audit.has("auth.account_unlocked") {
		t.Fatal("unlock must be audited")
	}
}

func TestLockoutDoublesOnConsecutiveLocks(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	for i := 0; i < 5; i++ {
		_, _ = h.login("wrong")
	}
	h.clock = h.clock.Add(16 * time.Minute)
	var err error
	for i := 0; i < 5; i++ {
		_, err = h.login("wrong")
	}
	var app *shared.AppError
	if !errors.As(err, &app) || app.RetryAfter != 30*time.Minute {
		t.Fatalf("second lock must be 30m, got %v", err)
	}
}

func TestUnknownEmailIsIndistinguishable(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	var err error
	for i := 0; i < 5; i++ {
		_, err = h.svc.Login(context.Background(), LoginInput{Email: "ghost@wodi.test", Password: "x", IP: "10.0.0.2"})
		if i < 4 && (!errors.Is(err, shared.ErrUnauthorized) || err.Error() != errInvalidCredentials.Error()) {
			t.Fatalf("attempt %d must be generic: %v", i, err)
		}
	}
	if !errors.Is(err, shared.ErrLocked) {
		t.Fatalf("unknown email must also lock after threshold: %v", err)
	}
}

func (h *harness) mustLogin(t *testing.T) (*platformauth.TokenPair, *platformauth.Claims) {
	t.Helper()
	res, err := h.login("correct-horse")
	if err != nil || res.Tokens == nil {
		t.Fatalf("login: %v", err)
	}
	claims, err := h.tokens.ParseAccess(res.Tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return res.Tokens, claims
}

func (h *harness) refresh(tok string) (*platformauth.TokenPair, error) {
	return h.svc.Refresh(context.Background(), RefreshInput{RefreshToken: tok, IP: "10.0.0.9"})
}

func TestLoginStartsSessionBoundToAccessToken(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	h.users.byID[h.user.ID].TokenVersion = 4
	pair, claims := h.mustLogin(t)
	sess := h.sec.sessions[claims.SessionID]
	if sess == nil || claims.TokenVersion != 4 {
		t.Fatalf("access token must carry sid of a stored session and ver: %+v", claims)
	}
	if sess.AuthMethod != authsec.AuthMethodPassword || !sess.AbsoluteExpiresAt.Equal(h.clock.Add(testAbsolute)) {
		t.Fatalf("session: %+v", sess)
	}
	if !authsec.IsRefreshToken(pair.RefreshToken) || strings.Count(pair.RefreshToken, ".") != 0 {
		t.Fatalf("refresh token must be opaque: %q", pair.RefreshToken)
	}
	if !pair.RefreshExpiresAt.Equal(h.clock.Add(testIdle)) {
		t.Fatalf("refresh expiry must be the idle window: %s", pair.RefreshExpiresAt)
	}
	for hash, rec := range h.sec.refresh {
		if hash == pair.RefreshToken || rec.FamilyID != sess.ID {
			t.Fatal("only the hash is stored, under the session")
		}
	}
}

func TestRefreshRotationAndReuse(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	first, claims := h.mustLogin(t)
	second, err := h.refresh(first.RefreshToken)
	if err != nil || second.RefreshToken == first.RefreshToken {
		t.Fatalf("rotate: %v", err)
	}
	next, _ := h.tokens.ParseAccess(second.AccessToken)
	if next.SessionID != claims.SessionID {
		t.Fatal("rotation must stay in the same session")
	}
	h.clock = h.clock.Add(testGrace + time.Second)
	if _, err := h.refresh(first.RefreshToken); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("reuse must be rejected: %v", err)
	}
	if !h.audit.has("auth.refresh_reuse") {
		t.Fatal("reuse must be audited")
	}
	if s := h.sec.sessions[claims.SessionID]; s.RevokedAt == nil || s.RevokedReason != authsec.RevokeReuse {
		t.Fatalf("reuse must revoke the session: %+v", s)
	}
	if !h.cache.sawSession(claims.SessionID) {
		t.Fatal("reuse must invalidate cached session checks")
	}
	if _, err := h.refresh(second.RefreshToken); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("reuse must revoke the whole family: %v", err)
	}
}

func TestRefreshGraceWindow(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	first, claims := h.mustLogin(t)
	second, err := h.refresh(first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	h.clock = h.clock.Add(2 * time.Second)
	sibling, err := h.refresh(first.RefreshToken)
	if err != nil || sibling.RefreshToken == second.RefreshToken {
		t.Fatalf("replay inside grace must yield a fresh token: %v", err)
	}
	if !h.audit.has("auth.refresh_grace") || h.audit.has("auth.refresh_reuse") {
		t.Fatalf("grace must be audited as benign: %v", h.audit.actions)
	}
	if h.sec.sessions[claims.SessionID].RevokedAt != nil {
		t.Fatal("grace must keep the session")
	}
	if _, err := h.refresh(second.RefreshToken); err != nil {
		t.Fatalf("the first successor stays valid: %v", err)
	}
	if _, err := h.refresh(sibling.RefreshToken); err != nil {
		t.Fatalf("the grace sibling is valid: %v", err)
	}
}

func TestConcurrentRefreshRace(t *testing.T) {
	for _, tc := range []struct {
		name  string
		grace time.Duration
		ok    bool
	}{{"grace on", testGrace, true}, {"grace off", 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, platformauth.RoleEmployee)
			h.svc.opts.ReuseGrace = tc.grace
			first, claims := h.mustLogin(t)
			rec := h.sec.refresh[authsec.HashToken(first.RefreshToken)]
			h.sec.beforeRotate = func() {
				now, other := h.clock, uuid.New()
				rec.RevokedAt, rec.RevokedReason, rec.ReplacedBy = &now, authsec.RevokeRotated, &other
			}
			_, err := h.refresh(first.RefreshToken)
			if tc.ok != (err == nil) {
				t.Fatalf("race: %v", err)
			}
			if revoked := h.sec.sessions[claims.SessionID].RevokedAt != nil; revoked == tc.ok {
				t.Fatalf("session revoked=%v", revoked)
			}
		})
	}
}

func TestSessionIdleSlidesUntilAbsolute(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	start := h.clock
	pair, claims := h.mustLogin(t)
	var err error
	for _, step := range []time.Duration{50 * time.Minute, 50 * time.Minute} {
		h.clock = h.clock.Add(step)
		if pair, err = h.refresh(pair.RefreshToken); err != nil {
			t.Fatalf("refresh at +%s: %v", h.clock.Sub(start), err)
		}
	}
	if want := h.clock.Add(testIdle); !pair.RefreshExpiresAt.Equal(want) {
		t.Fatalf("idle must slide: got %s want %s", pair.RefreshExpiresAt, want)
	}
	h.clock = start.Add(2*time.Hour + 30*time.Minute)
	if pair, err = h.refresh(pair.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if want := start.Add(testAbsolute); !pair.RefreshExpiresAt.Equal(want) {
		t.Fatalf("absolute must cap the successor: got %s want %s", pair.RefreshExpiresAt, want)
	}
	if s := h.sec.sessions[claims.SessionID]; !s.IdleExpiresAt.Equal(pair.RefreshExpiresAt) || s.LastIP != "10.0.0.9" {
		t.Fatalf("session must track the latest refresh: %+v", s)
	}
	h.clock = start.Add(testAbsolute + time.Second)
	if _, err := h.refresh(pair.RefreshToken); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("past absolute lifetime must fail: %v", err)
	}
	if s := h.sec.sessions[claims.SessionID]; s.RevokedReason != authsec.RevokeExpired || !h.audit.has("auth.session_expired") {
		t.Fatalf("expired session must be revoked and audited: %+v", s)
	}
}

func TestRefreshAfterIdleExpiry(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	pair, claims := h.mustLogin(t)
	h.clock = h.clock.Add(testIdle + time.Second)
	if _, err := h.refresh(pair.RefreshToken); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("idle session must not refresh: %v", err)
	}
	if h.sec.sessions[claims.SessionID].RevokedReason != authsec.RevokeExpired {
		t.Fatal("idle expiry ends the session")
	}
}

func TestRefreshRejectsMalformedAndInactive(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	for _, bad := range []string{"", "eyJhbGciOiJIUzI1NiJ9.e30.x", "wrt_unknown"} {
		if _, err := h.refresh(bad); !errors.Is(err, shared.ErrUnauthorized) {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	pair, claims := h.mustLogin(t)
	h.users.byID[h.user.ID].IsActive = false
	if _, err := h.refresh(pair.RefreshToken); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("inactive user: %v", err)
	}
	if h.sec.sessions[claims.SessionID].RevokedReason != authsec.RevokeUserDeactivated {
		t.Fatal("inactive user's session must end")
	}
}

func TestLogoutRevokesSession(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	ctx := context.Background()
	current, claims := h.mustLogin(t)
	other, otherClaims := h.mustLogin(t)
	err := h.svc.Logout(ctx, LogoutInput{UserID: h.user.ID, SessionID: claims.SessionID, RefreshToken: other.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{current.RefreshToken, other.RefreshToken} {
		if _, err := h.refresh(tok); !errors.Is(err, shared.ErrUnauthorized) {
			t.Fatalf("logged-out token must not refresh: %v", err)
		}
	}
	if !h.cache.sawSession(claims.SessionID) || !h.cache.sawSession(otherClaims.SessionID) {
		t.Fatal("logout must invalidate cached checks")
	}
	if h.sec.sessions[claims.SessionID].RevokedReason != authsec.RevokeLogout {
		t.Fatal("reason must be logout")
	}
}

func TestLogoutWithRefreshTokenOnly(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	ctx := context.Background()
	pair, claims := h.mustLogin(t)
	if err := h.svc.Logout(ctx, LogoutInput{RefreshToken: pair.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.refresh(pair.RefreshToken); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("logged-out token must not refresh: %v", err)
	}
	if h.sec.sessions[claims.SessionID].RevokedReason != authsec.RevokeLogout {
		t.Fatal("reason must be logout")
	}
	if err := h.svc.Logout(ctx, LogoutInput{RefreshToken: "wrt_unknown"}); err != nil {
		t.Fatalf("unknown token must be a no-op: %v", err)
	}
}

func TestSessionManagement(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	ctx := context.Background()
	_, s1 := h.mustLogin(t)
	h.clock = h.clock.Add(time.Minute)
	_, s2 := h.mustLogin(t)
	h.clock = h.clock.Add(time.Minute)
	_, s3 := h.mustLogin(t)
	me := SessionActor{UserID: h.user.ID, CurrentSessionID: s3.SessionID}

	list, err := h.svc.ListSessions(ctx, h.user.ID)
	if err != nil || len(list) != 3 || list[0].ID != s3.SessionID {
		t.Fatalf("list newest first: %v %v", list, err)
	}
	stranger := RevokeSessionInput{SessionActor: SessionActor{UserID: uuid.New()}, SessionID: s1.SessionID}
	if err := h.svc.RevokeSession(ctx, stranger); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("other user's session must be 404: %v", err)
	}
	if err := h.svc.RevokeSession(ctx, RevokeSessionInput{SessionActor: me, SessionID: s1.SessionID}); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.RevokeSession(ctx, RevokeSessionInput{SessionActor: me, SessionID: s1.SessionID}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("already revoked must be 404: %v", err)
	}
	if h.sec.sessions[s1.SessionID].RevokedReason != authsec.RevokeUser || !h.audit.has("auth.session_revoked") {
		t.Fatal("revocation must be recorded")
	}

	n, err := h.svc.RevokeOtherSessions(ctx, me)
	if err != nil || n != 1 || h.sec.sessions[s2.SessionID].RevokedAt == nil || h.sec.sessions[s3.SessionID].RevokedAt != nil {
		t.Fatalf("revoke others: %d %v", n, err)
	}
	if !h.cache.sawSession(s2.SessionID) {
		t.Fatal("revoke-others must invalidate the cache")
	}

	admin := AdminRevokeSessionsInput{ActorID: uuid.New(), UserID: h.user.ID}
	if _, err := h.svc.AdminRevokeSessions(ctx, admin); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("admin revoke outside scope must be 404: %v", err)
	}
	n, err = h.svc.AdminRevokeSessions(access.WithScope(ctx, access.System()), admin)
	if err != nil || n != 1 || h.sec.sessions[s3.SessionID].RevokedReason != authsec.RevokeAdmin {
		t.Fatalf("admin revoke: %d %v", n, err)
	}
	if len(h.cache.users) != 1 || h.cache.users[0] != h.user.ID {
		t.Fatal("admin revoke must invalidate the user's cached checks")
	}

	h.clock = h.clock.Add(time.Minute)
	_, s4 := h.mustLogin(t)
	if err := h.svc.RevokeSession(ctx, RevokeSessionInput{SessionActor: SessionActor{UserID: h.user.ID, CurrentSessionID: s4.SessionID}, SessionID: s4.SessionID}); err != nil {
		t.Fatal(err)
	}
	if h.sec.sessions[s4.SessionID].RevokedReason != authsec.RevokeLogout {
		t.Fatal("revoking the current session is a logout")
	}
}

func codeFor(t *testing.T, secret string, at time.Time) string {
	t.Helper()
	key, err := totp.DecodeSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	return totp.Code(key, totp.Step(at))
}

func TestMFAEnrollVerifyReplayAndRecovery(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	ctx := context.Background()
	in := MFACodeInput{UserID: h.user.ID}

	enr, err := h.svc.Enroll(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ConfirmEnrollment(ctx, MFACodeInput{UserID: h.user.ID, Code: "000000"}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("dev code 000000 must be rejected: %v", err)
	}
	codes, err := h.svc.ConfirmEnrollment(ctx, MFACodeInput{UserID: h.user.ID, Code: codeFor(t, enr.Secret, h.clock)})
	if err != nil || len(codes) != authsec.RecoveryCodeCount {
		t.Fatalf("confirm: %v (%d codes)", err, len(codes))
	}

	h.clock = h.clock.Add(time.Minute)
	res, err := h.login("correct-horse")
	if err != nil || !res.MFARequired || res.Tokens != nil {
		t.Fatalf("login must require mfa: %+v %v", res, err)
	}
	code := codeFor(t, enr.Secret, h.clock)
	ok, err := h.svc.VerifyMFA(ctx, MFAVerifyInput{Challenge: res.MFAChallenge, Code: code})
	if err != nil || ok.Tokens == nil {
		t.Fatalf("verify: %v", err)
	}

	res2, _ := h.login("correct-horse")
	if _, err := h.svc.VerifyMFA(ctx, MFAVerifyInput{Challenge: res2.MFAChallenge, Code: code}); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("replayed code must fail: %v", err)
	}
	if _, err := h.svc.VerifyMFA(ctx, MFAVerifyInput{Challenge: res2.MFAChallenge, Code: codes[0]}); err != nil {
		t.Fatalf("recovery code: %v", err)
	}
	if !h.audit.has("auth.mfa_recovery_used") || !h.audit.has("auth.mfa_failed") {
		t.Fatalf("mfa events must be audited: %v", h.audit.actions)
	}
	res3, _ := h.login("correct-horse")
	if _, err := h.svc.VerifyMFA(ctx, MFAVerifyInput{Challenge: res3.MFAChallenge, Code: codes[0]}); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("recovery code must be single-use: %v", err)
	}
}

func TestChallengeAttemptLimit(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	ctx := context.Background()
	enr, _ := h.svc.Enroll(ctx, MFACodeInput{UserID: h.user.ID})
	_, _ = h.svc.ConfirmEnrollment(ctx, MFACodeInput{UserID: h.user.ID, Code: codeFor(t, enr.Secret, h.clock)})
	h.svc.opts.Lockout.MaxAttempts = 100
	h.clock = h.clock.Add(time.Minute)
	res, _ := h.login("correct-horse")
	for i := 0; i < maxChallengeAttempts; i++ {
		_, _ = h.svc.VerifyMFA(ctx, MFAVerifyInput{Challenge: res.MFAChallenge, Code: "111111"})
	}
	_, err := h.svc.VerifyMFA(ctx, MFAVerifyInput{Challenge: res.MFAChallenge, Code: codeFor(t, enr.Secret, h.clock)})
	if !errors.Is(err, shared.ErrUnauthorized) || err.Error() != errInvalidChallenge.Error() {
		t.Fatalf("challenge must be burned after %d attempts: %v", maxChallengeAttempts, err)
	}
}

func TestForcedRoleEnrollmentFlow(t *testing.T) {
	h := newHarness(t, platformauth.RoleAdmin, platformauth.RoleAdmin, platformauth.RoleGM)
	ctx := context.Background()
	res, err := h.login("correct-horse")
	if err != nil || !res.MFAEnrollmentRequired || res.Tokens != nil || res.EnrollmentToken == "" {
		t.Fatalf("forced role must get enrollment token only: %+v %v", res, err)
	}
	enr, err := h.svc.SetupStart(ctx, MFASetupInput{EnrollmentToken: res.EnrollmentToken})
	if err != nil {
		t.Fatal(err)
	}
	done, err := h.svc.SetupConfirm(ctx, MFASetupInput{EnrollmentToken: res.EnrollmentToken, Code: codeFor(t, enr.Secret, h.clock)})
	if err != nil || done.Tokens == nil || len(done.RecoveryCodes) != authsec.RecoveryCodeCount || !done.User.MFAEnabled {
		t.Fatalf("setup confirm: %+v %v", done, err)
	}
	if _, err := h.svc.SetupStart(ctx, MFASetupInput{EnrollmentToken: res.EnrollmentToken}); err == nil {
		t.Fatal("enrollment token must be single-use")
	}
	if err := h.svc.DisableMFA(ctx, MFACodeInput{UserID: h.user.ID, Password: "correct-horse", Code: "123456"}); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("forced role cannot disable mfa: %v", err)
	}
}

func TestDisableMFARequiresPasswordAndCode(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	ctx := context.Background()
	enr, _ := h.svc.Enroll(ctx, MFACodeInput{UserID: h.user.ID})
	_, _ = h.svc.ConfirmEnrollment(ctx, MFACodeInput{UserID: h.user.ID, Code: codeFor(t, enr.Secret, h.clock)})
	h.clock = h.clock.Add(time.Minute)
	code := codeFor(t, enr.Secret, h.clock)
	if err := h.svc.DisableMFA(ctx, MFACodeInput{UserID: h.user.ID, Password: "nope", Code: code}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := h.svc.DisableMFA(ctx, MFACodeInput{UserID: h.user.ID, Password: "correct-horse", Code: code}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if h.sec.state[h.user.ID].MFAEnabled || !h.audit.has("auth.mfa_disabled") {
		t.Fatal("mfa must be disabled and audited")
	}
}
