package ops

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/app/retention"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// SecurityCleaner purges expired security records.
type SecurityCleaner interface {
	Run(ctx context.Context, actorID uuid.UUID) (retention.Result, error)
}

// Handler exposes queue health / job status for ops (GM/Admin).
type Handler struct {
	Queue   shared.QueueInspector
	Cleanup SecurityCleaner
}

func (h Handler) QueueStats(w http.ResponseWriter, r *http.Request) {
	if h.Queue == nil {
		response.JSON(w, http.StatusOK, shared.QueueStats{Mode: "disabled"})
		return
	}
	stats, err := h.Queue.Stats(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, stats)
}

func (h Handler) JobStatus(w http.ResponseWriter, r *http.Request) {
	if h.Queue == nil {
		response.Error(w, shared.NewNotFound("job"))
		return
	}
	id := chi.URLParam(r, "id")
	queue := r.URL.Query().Get("queue")
	info, err := h.Queue.GetJob(r.Context(), queue, id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, info)
}

// SecurityCleanup runs the retention purge now; schedulers call it or
// enqueue the security.cleanup job.
func (h Handler) SecurityCleanup(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if h.Cleanup == nil {
		response.Error(w, shared.NewNotFound("security cleanup"))
		return
	}
	res, err := h.Cleanup.Run(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}
