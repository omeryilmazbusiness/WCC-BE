package inbox

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appinbox "github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type linkLeadRequest struct {
	LeadID string `json:"lead_id"`
}

// LinkLead handles POST /inbox/conversations/{id}/lead.
func (h Handler) LinkLead(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req linkLeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	leadID, err := uuid.Parse(req.LeadID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid lead_id"))
		return
	}
	c, err := h.Svc.LinkLead(r.Context(), appinbox.LinkLeadInput{
		ConversationID: id, LeadID: leadID, ActorID: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapConversation(c))
}
