package task

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
	Priority    string     `json:"priority"`
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

type completeRequest struct {
	Outcome string `json:"outcome"`
}

type assignRequest struct {
	AssigneeID string   `json:"assignee_id"`
	TaskIDs    []string `json:"task_ids"`
}

func mapTask(t *domain.Task) map[string]any {
	if t == nil {
		return nil
	}
	var due, esc, completed any
	if t.DueAt != nil {
		due = t.DueAt.UTC().Format(time.RFC3339Nano)
	}
	if t.EscalatedAt != nil {
		esc = t.EscalatedAt.UTC().Format(time.RFC3339Nano)
	}
	if t.CompletedAt != nil {
		completed = t.CompletedAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":              t.ID,
		"branch_id":       t.BranchID,
		"title":           t.Title,
		"kind":            t.Kind,
		"status":          t.Status,
		"priority":        t.Priority,
		"outcome":         t.Outcome,
		"assignee_id":     t.AssigneeID,
		"assignee_name":   t.AssigneeName,
		"related_type":    t.RelatedType,
		"related_id":      t.RelatedID,
		"due_at":          due,
		"escalated_at":    esc,
		"idempotency_key": t.IdempotencyKey,
		"created_at":      t.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":      t.UpdatedAt.UTC().Format(time.RFC3339Nano),
		"completed_at":    completed,
		"overdue":         t.IsOverdue(time.Now().UTC()),
	}
}

func mapTasks(items []domain.Task) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapTask(&items[i]))
	}
	return out
}

func parseDue(raw *string) (*time.Time, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, *raw)
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	q := r.URL.Query()
	// Related-only shortcut (legacy query shape).
	if rt := q.Get("related_type"); rt != "" {
		rid, err := uuid.Parse(q.Get("related_id"))
		if err != nil {
			response.Error(w, shared.NewValidation("related_id is required"))
			return
		}
		items, err := h.Svc.ListByRelated(r.Context(), rt, rid)
		if err != nil {
			response.Error(w, err)
			return
		}
		response.JSON(w, http.StatusOK, mapTasks(items))
		return
	}

	in := appsvc.ListInput{
		BranchID: claims.BranchID, Status: domain.Status(q.Get("status")),
		Kind: domain.Kind(q.Get("kind")), Query: q.Get("q"),
		OverdueOnly: q.Get("overdue") == "1" || q.Get("overdue") == "true",
		EscalatedOnly: q.Get("escalated") == "1" || q.Get("escalated") == "true",
	}
	if v := q.Get("assignee_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid assignee_id"))
			return
		}
		in.AssigneeID = &id
	}
	if v := q.Get("limit"); v != "" {
		n, _ := strconv.Atoi(v)
		in.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, _ := strconv.Atoi(v)
		in.Offset = n
	}
	items, total, err := h.Svc.List(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSONMeta(w, http.StatusOK, mapTasks(items), map[string]any{"total": total})
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
	response.JSONMeta(w, http.StatusOK, mapTasks(items), map[string]any{"total": total})
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
	due, err := parseDue(req.DueAt)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid due_at"))
		return
	}
	t, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID: claims.BranchID, Title: req.Title, Kind: domain.Kind(req.Kind),
		Priority: domain.Priority(req.Priority), AssigneeID: assignee,
		RelatedType: req.RelatedType, RelatedID: req.RelatedID, DueAt: due,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapTask(t))
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
	response.JSON(w, http.StatusOK, mapTask(t))
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
	t, err := h.Svc.Transition(r.Context(), id, domain.Status(strings.TrimSpace(req.Status)))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTask(t))
}

func (h Handler) Complete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req completeRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	t, err := h.Svc.Complete(r.Context(), id, appsvc.CompleteInput{Outcome: req.Outcome})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTask(t))
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
	due, err := parseDue(req.DueAt)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid due_at"))
		return
	}
	t, err := h.Svc.Reschedule(r.Context(), id, due)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTask(t))
}

func (h Handler) Assign(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	aid, err := uuid.Parse(req.AssigneeID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid assignee_id"))
		return
	}
	t, err := h.Svc.Assign(r.Context(), id, appsvc.AssignInput{AssigneeID: aid})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTask(t))
}

func (h Handler) BulkAssign(w http.ResponseWriter, r *http.Request) {
	var req assignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	aid, err := uuid.Parse(req.AssigneeID)
	if err != nil {
		response.Error(w, shared.NewValidation("invalid assignee_id"))
		return
	}
	ids := make([]uuid.UUID, 0, len(req.TaskIDs))
	for _, raw := range req.TaskIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid task_ids"))
			return
		}
		ids = append(ids, id)
	}
	items, err := h.Svc.BulkAssign(r.Context(), appsvc.BulkAssignInput{TaskIDs: ids, AssigneeID: aid})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTasks(items))
}

func (h Handler) EscalateOverdue(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	n, err := h.Svc.EscalateOverdue(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"escalated": n})
}

func (h Handler) SuggestNextTask(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	sug, err := h.Svc.SuggestNextTask(r.Context(), id, body.Outcome)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, sug)
}

func (h Handler) ConfirmNextTask(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		Outcome string `json:"outcome"`
		Title   string `json:"title"`
		Kind    string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	t, err := h.Svc.ConfirmNextTask(r.Context(), appsvc.ConfirmNextTaskInput{
		ConversationID: id, BranchID: claims.BranchID, ActorID: claims.UserID,
		Outcome: body.Outcome, Title: body.Title, Kind: body.Kind,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapTask(t))
}
