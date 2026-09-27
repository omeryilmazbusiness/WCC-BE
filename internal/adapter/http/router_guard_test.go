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
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// Routes reachable without a permission check, by design.
var permissionExempt = []string{
	"/v1/auth/",     // login, refresh, MFA and self-service endpoints
	"/v1/webhooks/", // authenticated by provider signature instead
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
	return NewRouter(cfg, tokens, sessions, Handlers{}), tokens
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
	mustGuard := map[string]bool{"POST /v1/users/{id}/sessions/revoke": false, "POST /v1/ops/security-cleanup": false}

	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatal("router does not expose routes")
	}
	checked := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/v1/") || exempt(route) || method == http.MethodOptions {
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
