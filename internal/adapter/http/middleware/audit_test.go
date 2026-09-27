package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type stubTokens struct{ c *platformauth.Claims }

func (s stubTokens) ParseAccess(string) (*platformauth.Claims, error) { return s.c, nil }

type stubSessions struct{}

func (stubSessions) Validate(context.Context, uuid.UUID, uuid.UUID, int) error { return nil }

func captureActor(got *audit.Actor) http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		*got, _ = audit.ActorFrom(r.Context())
	})
}

func TestAuthenticateSetsUserActor(t *testing.T) {
	c := &platformauth.Claims{UserID: uuid.New(), SessionID: uuid.New(), BranchID: uuid.New(), Role: platformauth.RoleEmployee}
	var got audit.Actor
	h := RequestID(AuditContext(Authenticate(stubTokens{c}, stubSessions{})(captureActor(&got))))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer t")
	req.Header.Set("User-Agent", "agent/1")
	req.RemoteAddr = "203.0.113.9:1234"
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !got.Authenticated() || got.UserID != c.UserID || got.SessionID != c.SessionID || got.BranchID != c.BranchID {
		t.Fatalf("actor: %+v", got)
	}
	if got.IP != "203.0.113.9" || got.UserAgent != "agent/1" || got.RequestID == "" {
		t.Fatalf("request metadata: %+v", got)
	}
}

func TestWebhookActor(t *testing.T) {
	var got audit.Actor
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "198.51.100.7:443"
	WebhookActor(captureActor(&got)).ServeHTTP(httptest.NewRecorder(), req)
	if got.Type != audit.ActorWebhook || got.UserID != uuid.Nil || got.IP != "198.51.100.7" {
		t.Fatalf("actor: %+v", got)
	}
}
