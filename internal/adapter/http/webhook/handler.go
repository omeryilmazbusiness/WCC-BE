package webhook

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appinbox "github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Handler ingests provider webhooks (T-098…T-103).
type Handler struct {
	Svc             *appinbox.Service
	DefaultBranchID uuid.UUID
}

func (h Handler) Ingest(w http.ResponseWriter, r *http.Request) {
	providerName := strings.ToLower(strings.TrimSpace(chi.URLParam(r, "provider")))
	if providerName == "" {
		response.Error(w, shared.NewValidation("provider is required"))
		return
	}
	channel := domain.Channel(providerName)
	if !domain.ValidChannel(channel) {
		response.Error(w, shared.NewValidation("unknown provider"))
		return
	}

	branchID := h.DefaultBranchID
	if v := r.URL.Query().Get("branch_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid branch_id"))
			return
		}
		branchID = id
	}
	if branchID == uuid.Nil {
		response.Error(w, shared.NewValidation("branch_id is required"))
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		response.Error(w, shared.NewValidation("unable to read body"))
		return
	}
	headers := map[string]string{}
	for k, vals := range r.Header {
		if len(vals) > 0 {
			headers[strings.ToLower(k)] = vals[0]
		}
	}

	msg, err := h.Svc.IngestWebhook(r.Context(), channel, branchID, headers, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id":                msg.ID,
		"conversation_id":   msg.ConversationID,
		"provider_event_id": msg.ProviderEventID,
		"status":            msg.Status,
		"direction":         msg.Direction,
	})
}
