package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/outbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// OutboxAdmin exposes outbox health and dead-letter recovery (T-281).
type OutboxAdmin interface {
	Stats(ctx context.Context) (domain.Stats, error)
	Dead(ctx context.Context, limit int) ([]domain.Record, error)
	Requeue(ctx context.Context, id uuid.UUID) error
}

type outboxRecord struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Payload   json.RawMessage `json:"payload"`
	BranchID  *uuid.UUID      `json:"branch_id,omitempty"`
	Attempts  int             `json:"attempts"`
	LastError string          `json:"last_error"`
	CreatedAt time.Time       `json:"created_at"`
}

// OutboxStats: GET /v1/ops/outbox.
func (h Handler) OutboxStats(w http.ResponseWriter, r *http.Request) {
	if h.Outbox == nil {
		response.Error(w, shared.NewNotFound("outbox"))
		return
	}
	st, err := h.Outbox.Stats(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, st)
}

// OutboxDead: GET /v1/ops/outbox/dead?limit=.
func (h Handler) OutboxDead(w http.ResponseWriter, r *http.Request) {
	if h.Outbox == nil {
		response.Error(w, shared.NewNotFound("outbox"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	recs, err := h.Outbox.Dead(r.Context(), limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]outboxRecord, 0, len(recs))
	for _, rec := range recs {
		out = append(out, outboxRecord{
			ID: rec.ID, Name: rec.Name, Payload: json.RawMessage(rec.Payload), BranchID: rec.BranchID,
			Attempts: rec.Attempts, LastError: rec.LastError, CreatedAt: rec.CreatedAt,
		})
	}
	response.JSON(w, http.StatusOK, out)
}

// OutboxRequeue: POST /v1/ops/outbox/{id}/requeue puts a dead event back in line.
func (h Handler) OutboxRequeue(w http.ResponseWriter, r *http.Request) {
	if h.Outbox == nil {
		response.Error(w, shared.NewNotFound("outbox"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	if err := h.Outbox.Requeue(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"status": "requeued"})
}
