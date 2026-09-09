package booking

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
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

type updateRequest struct {
	PaxCount    int    `json:"pax_count"`
	TotalAmount int64  `json:"total_amount"`
	Currency    string `json:"currency"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type participantRequest struct {
	FullName    string  `json:"full_name"`
	PassportNo  string  `json:"passport_no"`
	Nationality string  `json:"nationality"`
	DateOfBirth *string `json:"date_of_birth"`
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

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	b, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, b)
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	b, err := h.Svc.Update(r.Context(), id, appsvc.UpdateInput{
		PaxCount:    req.PaxCount,
		TotalAmount: req.TotalAmount,
		Currency:    req.Currency,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, b)
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

func (h Handler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	b, err := h.Svc.Transition(r.Context(), id, domain.Status(req.Status))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, b)
}

func (h Handler) AddParticipant(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req participantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	var dob *time.Time
	if req.DateOfBirth != nil && *req.DateOfBirth != "" {
		t, err := time.Parse("2006-01-02", *req.DateOfBirth)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid date_of_birth"))
			return
		}
		dob = &t
	}
	p, err := h.Svc.AddParticipant(r.Context(), id, appsvc.AddParticipantInput{
		FullName:    req.FullName,
		PassportNo:  req.PassportNo,
		Nationality: req.Nationality,
		DateOfBirth: dob,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, p)
}

func (h Handler) ListParticipants(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	items, err := h.Svc.ListParticipants(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}
