package httpadapter

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// Routes reachable without a permission check, by design.
var permissionExempt = []string{
	"/v1/auth/",     // login, refresh, MFA and self-service endpoints
	"/v1/webhooks/", // authenticated by provider signature instead
	"/v1/public/",   // pre sign-in company branding (no tenant data)
}

// Exact routes open to every authenticated caller, by design.
var authOnly = map[string]bool{
	"GET /v1/fx/live":                 true, // public market rates
	"GET /v1/stream":                  true, // user-scoped realtime signals, filtered by scope
	"GET /v1/me/preferences":          true, // the caller's own UI preferences
	"PUT /v1/me/preferences":          true,
	"POST /v1/me/preferences/welcome": true,
}

func exempt(pattern string) bool {
	for _, p := range permissionExempt {
		if strings.HasPrefix(pattern, p) {
			return true
		}
	}
	return false
}

// sessionsFunc is a test-only SessionValidator; production always checks the store.
type sessionsFunc func(sid, uid uuid.UUID, ver int) error

func (f sessionsFunc) Validate(_ context.Context, sid, uid uuid.UUID, ver int) error {
	return f(sid, uid, ver)
}

var allowAllSessions = sessionsFunc(func(uuid.UUID, uuid.UUID, int) error { return nil })

// anyWorkspace places every caller in a one-branch company around their home branch.
type anyWorkspace struct{}

func (anyWorkspace) Workspace(_ context.Context, branchID uuid.UUID) (*company.Workspace, error) {
	id := uuid.New()
	return &company.Workspace{
		Company:  company.Company{ID: id, Slug: "test-co", NameEN: "Test", IsActive: true},
		Branches: []company.Branch{{ID: branchID, CompanyID: id, Slug: "main", Kind: company.KindMainCenter}},
	}, nil
}

func testRouterWith(t *testing.T, sessions middleware.SessionValidator) (http.Handler, *platformauth.TokenService) {
	t.Helper()
	cfg := config.Config{
		App:  config.AppConfig{Env: "test"},
		HTTP: config.HTTPConfig{WriteTimeout: 5 * time.Second, MaxBodyBytes: 1 << 20},
		Auth: config.AuthConfig{
			JWTAccessSecret: "test-access-secret-test-access-secret", JWTAccessKeyID: "t1",
			AccessTTL: time.Minute, RefreshTTL: time.Hour, Issuer: "test", Audience: "test-api",
		},
	}
	tokens, err := platformauth.NewTokenServiceFromConfig(cfg.Auth)
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(cfg, tokens, sessions, anyWorkspace{}, Handlers{}), tokens
}

func testRouter(t *testing.T) (http.Handler, *platformauth.TokenService) {
	return testRouterWith(t, allowAllSessions)
}

func accessToken(t *testing.T, tokens *platformauth.TokenService, role platformauth.Role) string {
	t.Helper()
	at, err := tokens.IssueAccess(platformauth.AccessSubject{
		UserID: uuid.New(), Email: "nobody@test", Role: role, BranchID: uuid.New(), SessionID: uuid.New(), TokenVersion: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return at.Token
}

// TestEveryBusinessRouteRequiresPermission fails when a /v1 route is added
// without RequirePermission: a caller holding no permissions must get 403
// before any handler runs.
func TestEveryBusinessRouteRequiresPermission(t *testing.T) {
	router, tokens := testRouter(t)
	token := accessToken(t, tokens, platformauth.Role("nobody"))
	mustGuard := map[string]bool{
		"POST /v1/users/{id}/sessions/revoke": false, "POST /v1/ops/security-cleanup": false,
		"GET /v1/audit-events": false, "GET /v1/audit-events/export.csv": false, "GET /v1/audit-events/actions": false,
		"POST /v1/ops/encrypt-backfill": false, "POST /v1/customers/{id}/reveal-passport": false,
		"GET /v1/customers/{id}/export": false, "POST /v1/customers/{id}/anonymize": false,
		"POST /v1/bookings/{id}/participants/{participantId}/reveal-passport": false,
		"GET /v1/fx-rates/": false, "POST /v1/fx-rates/": false, "GET /v1/fx-rates/convert": false,
		"PUT /v1/fx-rates/{id}": false, "DELETE /v1/fx-rates/{id}": false,
		"POST /v1/fx-rates/adopt": false, "POST /v1/fx/live/refresh": false,
		"GET /v1/bookings/{id}/payment-promises": false, "POST /v1/bookings/{id}/payment-promises": false,
		"POST /v1/payment-promises/{id}/cancel": false,
		"POST /v1/bookings/{id}/status":         false, "POST /v1/bookings/{id}/readiness-override": false,
		"GET /v1/ai/leads/{id}/score": false, "POST /v1/ai/leads/{id}/score": false,
		"POST /v1/tasks/escalate-overdue": false,
		"POST /v1/branches":               false, "PATCH /v1/branches/{id}": false,
		"GET /v1/platform/companies/": false, "POST /v1/platform/companies/": false,
		"PUT /v1/platform/companies/{id}/logo": false, "DELETE /v1/platform/companies/{id}/logo": false,
		"GET /v1/flights/places": false, "GET /v1/flights/search": false,
	}

	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose routes")
	}
	checked := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/v1/") || exempt(route) || authOnly[method+" "+route] || method == http.MethodOptions {
			return nil
		}
		path := strings.NewReplacer(
			"{id}", uuid.NewString(), "{participantId}", uuid.NewString(), "{itemId}", uuid.NewString(),
			"{companionId}", uuid.NewString(), "{linkId}", uuid.NewString(), "{roomId}", uuid.NewString(),
			"{kind}", "overdue", "{provider}", "whatsapp",
		).Replace(route)
		path = strings.TrimSuffix(path, "/")
		if path == "" {
			path = "/"
		}
		req := httptest.NewRequest(method, path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s %s reached its handler without a permission check", method, route)
				}
			}()
			router.ServeHTTP(rec, req)
		}()
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: want 403 for a caller without permissions, got %d", method, route, rec.Code)
		}
		if _, ok := mustGuard[method+" "+route]; ok {
			mustGuard[method+" "+route] = true
		}
		checked++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 100 {
		t.Fatalf("walked only %d routes; router walk is broken", checked)
	}
	for route, seen := range mustGuard {
		if !seen {
			t.Errorf("%s must be registered and permission-guarded", route)
		}
	}
}

func TestSessionRoutesAreAuthenticated(t *testing.T) {
	router, _ := testRouter(t)
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/v1/auth/sessions"},
		{http.MethodDelete, "/v1/auth/sessions/" + uuid.NewString()},
		{http.MethodPost, "/v1/auth/sessions/revoke-others"},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(rt.method, rt.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: want 401, got %d", rt.method, rt.path, rec.Code)
		}
	}
}

func TestSessionCheckOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"revoked", authsec.ErrSessionRevoked, http.StatusUnauthorized, "session_revoked"},
		{"stale", authsec.ErrTokenStale, http.StatusUnauthorized, "token_stale"},
		{"store down fails closed", errors.New("db down"), http.StatusServiceUnavailable, "service_unavailable"},
	}
	for _, c := range cases {
		var gotSID uuid.UUID
		router, tokens := testRouterWith(t, sessionsFunc(func(sid, _ uuid.UUID, _ int) error { gotSID = sid; return c.err }))
		req := httptest.NewRequest(http.MethodGet, "/v1/customers/", nil)
		req.Header.Set("Authorization", "Bearer "+accessToken(t, tokens, platformauth.RoleGM))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var body struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if rec.Code != c.status || body.Error.Code != c.code || gotSID == uuid.Nil {
			t.Errorf("%s: got %d %q (sid %s), want %d %q", c.name, rec.Code, body.Error.Code, gotSID, c.status, c.code)
		}
	}
}

func TestLegacyTokenWithoutKidIsRejected(t *testing.T) {
	router, _ := testRouter(t)
	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid": uuid.NewString(), "sub": "x", "role": "gm", "iss": "test", "exp": time.Now().Add(time.Minute).Unix(),
	})
	raw, _ := legacy.SignedString([]byte("test-access-secret-test-access-secret"))
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("pre-session token must be rejected, got %d", rec.Code)
	}
}

func TestBusinessRoutesRequireAuthentication(t *testing.T) {
	router, _ := testRouter(t)
	for _, path := range []string{"/v1/customers/", "/v1/bookings/", "/v1/leads/", "/v1/dashboard/my-target"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without token: want 401, got %d", path, rec.Code)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	router, _ := testRouter(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/customers/", nil))
	for _, h := range []string{"X-Content-Type-Options", "X-Frame-Options", "Content-Security-Policy", "Strict-Transport-Security"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("missing security header %s", h)
		}
	}
}

// TestDataProtectionRoutesRequireTheirPermission covers roles that hold the
// neighbouring permissions but not the one guarding Epic 20 routes: reveals
// need pii.read on top of read access, KVKK requests need privacy.manage.
func TestDataProtectionRoutesRequireTheirPermission(t *testing.T) {
	router, tokens := testRouter(t)
	id, pid := uuid.NewString(), uuid.NewString()
	cases := []struct {
		role         platformauth.Role
		method, path string
	}{
		{platformauth.RoleEmployee, http.MethodPost, "/v1/customers/" + id + "/reveal-passport"},
		{platformauth.RoleFinance, http.MethodPost, "/v1/customers/" + id + "/reveal-passport"},
		{platformauth.RoleEmployee, http.MethodPost, "/v1/bookings/" + id + "/participants/" + pid + "/reveal-passport"},
		{platformauth.RoleAdmin, http.MethodPost, "/v1/customers/" + id + "/reveal-passport"},
		{platformauth.RoleManager, http.MethodGet, "/v1/customers/" + id + "/export"},
		{platformauth.RoleManager, http.MethodPost, "/v1/customers/" + id + "/anonymize"},
		{platformauth.RoleOperations, http.MethodPost, "/v1/customers/" + id + "/anonymize"},
		{platformauth.RoleManager, http.MethodPost, "/v1/ops/encrypt-backfill"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"reason":"customer request by email"}`))
		req.Header.Set("Authorization", "Bearer "+accessToken(t, tokens, c.role))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as %s: want 403, got %d", c.method, c.path, c.role, rec.Code)
		}
	}
}

// Epic 21 T-271 — booking writers without bookings.override can neither hit
// the readiness override route nor request an override status change.
func TestBookingOverrideRequiresPermission(t *testing.T) {
	router, tokens := testRouter(t)
	id := uuid.NewString()
	for _, role := range []platformauth.Role{platformauth.RoleEmployee, platformauth.RoleFinance, platformauth.RoleOperations} {
		for _, c := range []struct{ path, body string }{
			{"/v1/bookings/" + id + "/readiness-override", `{"reason":"documents arrive at the airport"}`},
			{"/v1/bookings/" + id + "/status", `{"status":"ready","reason":"documents arrive at the airport","override":true}`},
		} {
			req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body))
			req.Header.Set("Authorization", "Bearer "+accessToken(t, tokens, role))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("POST %s as %s: want 403, got %d", c.path, role, rec.Code)
			}
		}
	}
}

// Live rates: the board only needs authentication; refresh and adopt need
// fx.manage, which employees and operations do not hold.
func TestLiveFXRoutes(t *testing.T) {
	router, tokens := testRouter(t)
	for _, rt := range []struct{ method, path string }{
		{http.MethodGet, "/v1/fx/live"},
		{http.MethodPost, "/v1/fx/live/refresh"},
		{http.MethodPost, "/v1/fx-rates/adopt"},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(rt.method, rt.path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without token: want 401, got %d", rt.method, rt.path, rec.Code)
		}
	}
	for _, role := range []platformauth.Role{platformauth.RoleEmployee, platformauth.RoleOperations, platformauth.Role("nobody")} {
		for _, c := range []struct{ path, body string }{
			{"/v1/fx/live/refresh", `{}`},
			{"/v1/fx-rates/adopt", `{"currency":"USD","kind":"official","side":"mid"}`},
		} {
			req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body))
			req.Header.Set("Authorization", "Bearer "+accessToken(t, tokens, role))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Errorf("POST %s as %s: want 403, got %d", c.path, role, rec.Code)
			}
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/fx/live", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken(t, tokens, platformauth.Role("nobody")))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "fx_live_disabled") {
		t.Errorf("GET /v1/fx/live for any authenticated caller reaches the handler (disabled here): %d %s", rec.Code, rec.Body)
	}
}
