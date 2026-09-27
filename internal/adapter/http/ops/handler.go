package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/app/dataprotection"
	"github.com/wodi-crm/wodi-crm-be/internal/app/retention"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// EncryptBackfiller moves legacy plaintext secrets/passports under encryption.
type EncryptBackfiller interface {
	Run(ctx context.Context, actorID uuid.UUID, opt dataprotection.Options) (dataprotection.Result, error)
}

// SecurityCleaner purges expired security records.
type SecurityCleaner interface {
	Run(ctx context.Context, actorID uuid.UUID) (retention.Result, error)
}

// Handler exposes queue health / job status for ops (GM/Admin).
type Handler struct {
	Queue    shared.QueueInspector
	Cleanup  SecurityCleaner
	Backfill EncryptBackfiller
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

// EncryptBackfill runs the Epic 20 encrypt backfill now (idempotent). The
// optional body {"rehash":true} rebuilds passport blind indexes.
func (h Handler) EncryptBackfill(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if h.Backfill == nil {
		response.Error(w, shared.NewNotFound("encrypt backfill"))
		return
	}
	var body struct {
		Rehash bool `json:"rehash"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			response.Error(w, shared.NewValidation("invalid json"))
			return
		}
	}
	res, err := h.Backfill.Run(r.Context(), claims.UserID, dataprotection.Options{Rehash: body.Rehash})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}
