package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// AuditContext attaches the request's IP, user agent and request id to the
// audit actor so unauthenticated flows (login, refresh) are attributed too.
// It must run after RequestID and TrustedRealIP; Authenticate upgrades the
// actor to the verified user.
func AuditContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(audit.WithActor(r.Context(), requestActor(r, ""))))
	})
}

// WebhookActor attributes provider webhook ingress to the webhook actor.
func WebhookActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(audit.WithActor(r.Context(), requestActor(r, audit.ActorWebhook))))
	})
}

func requestActor(r *http.Request, t audit.ActorType) audit.Actor {
	return audit.Actor{
		Type:      t,
		IP:        ClientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: middleware.GetReqID(r.Context()),
	}
}

func userActor(r *http.Request, c *platformauth.Claims) audit.Actor {
	a := requestActor(r, audit.ActorUser)
	a.UserID, a.SessionID, a.BranchID = c.UserID, c.SessionID, c.BranchID
	return a
}
