package health

import (
	"context"
	"net/http"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
)

type Checker interface {
	Ping(ctx context.Context) error
}

type Handler struct {
	DB      Checker
	Queue   Checker
	Version string
	Env     string
}

func (h Handler) Live(w http.ResponseWriter, _ *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	status := http.StatusOK
	dbOK := true
	queueOK := true

	if h.DB != nil {
		if err := h.DB.Ping(ctx); err != nil {
			dbOK = false
			status = http.StatusServiceUnavailable
		}
	}
	if h.Queue != nil {
		if err := h.Queue.Ping(ctx); err != nil {
			queueOK = false
			// Queue down is degraded but API can still serve reads;
			// mark not ready in production-sensitive readiness.
			status = http.StatusServiceUnavailable
		}
	}

	ready := dbOK && queueOK
	response.JSON(w, status, map[string]any{
		"status":  map[bool]string{true: "ready", false: "not_ready"}[ready],
		"db":      dbOK,
		"queue":   queueOK,
		"version": h.Version,
		"env":     h.Env,
	})
}
