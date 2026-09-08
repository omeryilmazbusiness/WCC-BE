package lead

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type createRequest struct {
	CustomerID *uuid.UUID `json:"customer_id"`
	FullName   string     `json:"full_name"`
	Phone      string     `json:"phone"`
	Source     string     `json:"source"`
	Notes      string     `json:"notes"`
	OwnerID    *uuid.UUID `json:"owner_id"`
}

type stageRequest struct {
	Stage string `json:"stage"`
	Note  string `json:"note"`
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
	owner := claims.UserID
	if req.OwnerID != nil {
		owner = *req.OwnerID
	}
	l, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID:   claims.BranchID,
		CustomerID: req.CustomerID,
		FullName:   req.FullName,
		Phone:      req.Phone,
		Source:     req.Source,
		OwnerID:    owner,
		Notes:      req.Notes,
		ActorID:    claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, l)
}

func (h Handler) ChangeStage(w http.ResponseWriter, r *http.Request) {
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
	var req stageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	l, err := h.Svc.ChangeStage(r.Context(), id, domain.Stage(req.Stage), claims.UserID, req.Note)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, l)
}
