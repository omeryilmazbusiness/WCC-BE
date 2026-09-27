// Package stream serves the realtime Server-Sent Events feed (T-288).
package stream

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/app/realtime"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Subscriptions is the hub side of the stream (satisfied by realtime.Hub).
type Subscriptions interface {
	Subscribe(sub realtime.Subscriber) (<-chan realtime.Signal, func(), bool)
}

const (
	defaultHeartbeat = 25 * time.Second
	// retryMillis tells EventSource-style clients how long to wait before reconnecting.
	retryMillis = 3000
)

type Handler struct {
	Hub       Subscriptions
	Heartbeat time.Duration
	// Closing ends every open stream when closed; http.Server.Shutdown does not
	// cancel in-flight requests, so without it streams would block shutdown.
	Closing <-chan struct{}
}

// Stream: GET /v1/stream. Signals carry no record data, only what changed,
// so clients refetch through the normal scoped endpoints. The stream ends
// when the access token expires; the client reconnects with a fresh one.
func (h Handler) Stream(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if h.Hub == nil {
		response.Error(w, shared.NewUnavailable("realtime stream disabled", 30*time.Second))
		return
	}
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		response.Error(w, shared.NewUnavailable("streaming not supported", 30*time.Second))
		return
	}
	signals, cancel, ok := h.Hub.Subscribe(realtime.Subscriber{UserID: claims.UserID, Scope: access.From(r.Context())})
	if !ok {
		response.Error(w, shared.NewUnavailable("too many open streams", 30*time.Second))
		return
	}
	defer cancel()

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	// no-transform keeps compressing proxies from buffering the stream.
	hdr.Set("Cache-Control", "no-store, no-transform")
	hdr.Set("Connection", "keep-alive")
	hdr.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "retry: %d\nevent: ready\ndata: {}\n\n", retryMillis); err != nil || rc.Flush() != nil {
		return
	}

	beat := h.Heartbeat
	if beat <= 0 {
		beat = defaultHeartbeat
	}
	ticker := time.NewTicker(beat)
	defer ticker.Stop()
	var expired <-chan time.Time
	if claims.ExpiresAt != nil {
		timer := time.NewTimer(time.Until(claims.ExpiresAt.Time))
		defer timer.Stop()
		expired = timer.C
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.Closing:
			return
		case <-expired:
			_, _ = fmt.Fprint(w, "event: expired\ndata: {}\n\n")
			_ = rc.Flush()
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case s := <-signals:
			raw, err := json.Marshal(s)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", s.Type, raw); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}
