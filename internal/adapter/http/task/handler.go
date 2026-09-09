package task

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

type Handler struct {
	Svc *appsvc.Service
}

type createRequest struct {
	Title       string     `json:"title"`
	Kind        string     `json:"kind"`
	AssigneeID  *uuid.UUID `json:"assignee_id"`
	RelatedType string     `json:"related_type"`
	RelatedID   uuid.UUID  `json:"related_id"`
	DueAt       *string    `json:"due_at"`
}

type statusRequest struct {
	Status string `json:"status"`
}

type rescheduleRequest struct {
	DueAt *string `json:"due_at"`
}

func (h Handler) ListMine(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	var status *domain.Status
	if v := r.URL.Query().Get("status"); v != "" {
		s := domain.Status(v)
		status = &s
	}
	items, total, err := h.Svc.ListMine(r.Context(), claims.UserID, status, limit, offset)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSONMeta(w, http.StatusOK, items, map[string]any{"total": total})
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
	assignee := claims.UserID
	if req.AssigneeID != nil {
		assignee = *req.AssigneeID
	}
	var due *time.Time
	if req.DueAt != nil && *req.DueAt != "" {
		t, err := time.Parse(time.RFC3339, *req.DueAt)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid due_at"))
			return
		}
		due = &t
	}
	t, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID:    claims.BranchID,
		Title:       req.Title,
		Kind:        domain.Kind(req.Kind),
		AssigneeID:  assignee,
		RelatedType: req.RelatedType,
		RelatedID:   req.RelatedID,
		DueAt:       due,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, t)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	t, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, t)
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
	t, err := h.Svc.Transition(r.Context(), id, domain.Status(req.Status))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, t)
}

func (h Handler) Complete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	t, err := h.Svc.Complete(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, t)
}

func (h Handler) Reschedule(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req rescheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	var due *time.Time
	if req.DueAt != nil && *req.DueAt != "" {
		t, err := time.Parse(time.RFC3339, *req.DueAt)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid due_at"))
			return
		}
		due = &t
	}
	t, err := h.Svc.Reschedule(r.Context(), id, due)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, t)
}

func (h Handler) ListByRelated(w http.ResponseWriter, r *http.Request) {
	relatedType := r.URL.Query().Get("related_type")
	relatedID, err := uuid.Parse(r.URL.Query().Get("related_id"))
	if relatedType == "" || err != nil {
		response.Error(w, shared.NewValidation("related_type and related_id are required"))
		return
	}
	items, err := h.Svc.ListByRelated(r.Context(), relatedType, relatedID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}
