package booking

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type createRequest struct {
	CustomerID  uuid.UUID  `json:"customer_id"`
	DepartureID uuid.UUID  `json:"departure_id"`
	LeadID      *uuid.UUID `json:"lead_id"`
	PaxCount    int        `json:"pax_count"`
	TotalAmount int64      `json:"total_amount"`
	Currency    string     `json:"currency"`
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	b, err := h.Svc.CreateDraft(r.Context(), appsvc.CreateInput{
		BranchID:    claims.BranchID,
		CustomerID:  req.CustomerID,
		DepartureID: req.DepartureID,
		LeadID:      req.LeadID,
		PaxCount:    req.PaxCount,
		TotalAmount: req.TotalAmount,
		Currency:    req.Currency,
		OwnerID:     claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, b)
}

func (h Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	b, err := h.Svc.Confirm(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, b)
}
