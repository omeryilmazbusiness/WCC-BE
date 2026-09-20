package ops

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Handler exposes queue health / job status for ops (GM/Admin).
type Handler struct {
	Queue shared.QueueInspector
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
