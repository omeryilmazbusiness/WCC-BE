package webhook

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appwebhook "github.com/wodi-crm/wodi-crm-be/internal/app/webhook"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/ratelimit"
)

// MaxBodyBytes caps raw webhook payloads (T-250).
const MaxBodyBytes = 1 << 20

// Handler ingests provider webhooks. The branch is resolved from the
// integration account named in the payload, never from the request.
type Handler struct {
	Svc *appwebhook.Service
	// Limiter throttles per source IP; RateLimit events per RateWindow.
	Limiter    ratelimit.Window
	RateLimit  int
	RateWindow time.Duration
}

func (h Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	ip := middleware.ClientIP(r)
	if err := h.throttle(r, ip); err != nil {
		response.Error(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(w, shared.NewValidation("payload exceeds 1MB"))
			return
		}
		response.Error(w, shared.NewValidation("unable to read body"))
		return
	}
	headers := make(map[string]string, len(r.Header))
	for k, vals := range r.Header {
		if len(vals) > 0 {
			headers[strings.ToLower(k)] = vals[0]
		}
	}

	res, err := h.Svc.Handle(r.Context(), appwebhook.Request{
		Provider: chi.URLParam(r, "provider"), Headers: headers, Body: body, IP: ip,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	if res.Duplicate || res.Message == nil {
		response.JSON(w, http.StatusOK, map[string]any{
			"webhook_event_id": res.EventID,
			"status":           "duplicate",
		})
		return
	}
	msg := res.Message
	response.JSON(w, http.StatusOK, map[string]any{
		"id":                msg.ID,
		"conversation_id":   msg.ConversationID,
		"provider_event_id": msg.ProviderEventID,
		"status":            msg.Status,
		"direction":         msg.Direction,
		"webhook_event_id":  res.EventID,
	})
}

// Verify answers the Meta subscription handshake with the raw challenge.
func (h Handler) Verify(w http.ResponseWriter, r *http.Request) {
	if err := h.throttle(r, middleware.ClientIP(r)); err != nil {
		response.Error(w, err)
		return
	}
	q := r.URL.Query()
	challenge, err := h.Svc.Handshake(r.Context(), chi.URLParam(r, "provider"),
		q.Get("hub.mode"), q.Get("hub.verify_token"), q.Get("hub.challenge"))
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, challenge)
}

func (h Handler) throttle(r *http.Request, ip string) error {
	if h.Limiter == nil || h.RateLimit <= 0 || ip == "" {
		return nil
	}
	n, _ := h.Limiter.Hit(r.Context(), "webhook:ip:"+ip, h.RateWindow)
	if n > h.RateLimit {
		return shared.NewRateLimited("too many webhook requests", h.RateWindow)
	}
	return nil
}
