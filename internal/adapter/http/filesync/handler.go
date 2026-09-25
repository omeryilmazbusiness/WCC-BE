package filesync

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/filesync"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/filesync"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func mapConn(c *domain.Connection) map[string]any {
	m := map[string]any{
		"id": c.ID, "branch_id": c.BranchID, "provider": c.Provider,
		"display_name": c.DisplayName, "remote_path": c.RemotePath,
		"entity_type": c.EntityType, "source_of_truth": c.SourceOfTruth,
		"conflict_policy": c.ConflictPolicy, "enabled": c.Enabled,
		"status": c.Status, "last_error": c.LastError,
		"config_json": c.ConfigJSON,
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if c.LastSyncAt != nil {
		m["last_sync_at"] = c.LastSyncAt.UTC().Format(time.RFC3339Nano)
	}
	if c.CreatedBy != nil {
		m["created_by"] = c.CreatedBy
	}
	return m
}

func mapRun(run *domain.Run) map[string]any {
	m := map[string]any{
		"id": run.ID, "connection_id": run.ConnectionID, "branch_id": run.BranchID,
		"direction": run.Direction, "status": run.Status,
		"rows_read": run.RowsRead, "rows_applied": run.RowsApplied, "conflicts": run.Conflicts,
		"summary_json": run.SummaryJSON, "error_message": run.ErrorMessage,
		"started_at": run.StartedAt.UTC().Format(time.RFC3339Nano),
	}
	if run.ActorID != nil {
		m["actor_id"] = run.ActorID
	}
	if run.FinishedAt != nil {
		m["finished_at"] = run.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return m
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.List(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapConn(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body appsvc.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	body.BranchID = claims.BranchID
	body.ActorID = claims.UserID
	c, err := h.Svc.Create(r.Context(), body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapConn(c))
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
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
	c, err := h.Svc.Get(r.Context(), claims.BranchID, id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapConn(c))
}

func (h Handler) Update(w http.ResponseWriter, r *http.Request) {
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
	var body appsvc.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	c, err := h.Svc.Update(r.Context(), claims.BranchID, id, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapConn(c))
}

func (h Handler) Delete(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Svc.Delete(r.Context(), claims.BranchID, id); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) Connect(w http.ResponseWriter, r *http.Request) {
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
	c, err := h.Svc.Connect(r.Context(), claims.BranchID, id, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapConn(c))
}

func (h Handler) Sync(w http.ResponseWriter, r *http.Request) {
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
	run, err := h.Svc.SyncNow(r.Context(), claims.BranchID, id, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapRun(run))
}

func (h Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var connID *uuid.UUID
	if q := r.URL.Query().Get("connection_id"); q != "" {
		id, err := uuid.Parse(q)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid connection_id"))
			return
		}
		connID = &id
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.ListRuns(r.Context(), claims.BranchID, connID, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapRun(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}
