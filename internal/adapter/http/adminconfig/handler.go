package adminconfig

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/adminconfig"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/adminconfig"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) GetSLA(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.GetSLA(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) PutSLA(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body []appsvc.UpsertSLAInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	items, err := h.Svc.PutSLA(r.Context(), claims.BranchID, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) GetEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.GetEscalationMatrix(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) PutEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	kind := chi.URLParam(r, "kind")
	var body appsvc.UpsertEscalationInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	o, err := h.Svc.UpsertEscalation(r.Context(), claims.BranchID, kind, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, o)
}

func (h Handler) DeleteEscalation(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if err := h.Svc.DeleteEscalation(r.Context(), claims.BranchID, chi.URLParam(r, "kind")); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) ListLostReasons(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	activeOnly := r.URL.Query().Get("active") == "1" || r.URL.Query().Get("active") == "true"
	items, err := h.Svc.GetLostReasons(r.Context(), claims.BranchID, activeOnly)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) CreateLostReason(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body appsvc.LostReasonInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	lr, err := h.Svc.CreateLostReason(r.Context(), claims.BranchID, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, lr)
}

func (h Handler) UpdateLostReason(w http.ResponseWriter, r *http.Request) {
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
	var body appsvc.LostReasonInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	lr, err := h.Svc.UpdateLostReason(r.Context(), claims.BranchID, id, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, lr)
}

func (h Handler) DeleteLostReason(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Svc.DeleteLostReason(r.Context(), claims.BranchID, id); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func mapTemplate(t *domain.MessageTemplate) map[string]any {
	return map[string]any{
		"id": t.ID, "branch_id": t.BranchID, "channel": t.Channel,
		"name": t.Name, "body": t.Body, "variables_json": json.RawMessage(t.VariablesJSON),
		"is_active": t.IsActive, "created_by": t.CreatedBy,
		"created_at": t.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": t.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (h Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.ListTemplates(r.Context(), claims.BranchID, r.URL.Query().Get("channel"))
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapTemplate(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		Channel       string          `json:"channel"`
		Name          string          `json:"name"`
		Body          string          `json:"body"`
		VariablesJSON json.RawMessage `json:"variables_json"`
		IsActive      *bool           `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	t, err := h.Svc.CreateTemplate(r.Context(), claims.BranchID, claims.UserID, appsvc.TemplateInput{
		Channel: body.Channel, Name: body.Name, Body: body.Body,
		VariablesJSON: body.VariablesJSON, IsActive: body.IsActive,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapTemplate(t))
}

func (h Handler) PatchTemplate(w http.ResponseWriter, r *http.Request) {
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
		Channel       string          `json:"channel"`
		Name          string          `json:"name"`
		Body          string          `json:"body"`
		VariablesJSON json.RawMessage `json:"variables_json"`
		IsActive      *bool           `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	t, err := h.Svc.PatchTemplate(r.Context(), claims.BranchID, id, appsvc.TemplateInput{
		Channel: body.Channel, Name: body.Name, Body: body.Body,
		VariablesJSON: body.VariablesJSON, IsActive: body.IsActive,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapTemplate(t))
}

func (h Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Svc.DeleteTemplate(r.Context(), claims.BranchID, id); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) GetFields(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.GetFields(r.Context(), claims.BranchID, r.URL.Query().Get("entity"))
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) PutFields(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	entity := r.URL.Query().Get("entity")
	var body []domain.FieldConfig
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	items, err := h.Svc.PutFields(r.Context(), claims.BranchID, entity, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) GetThresholds(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	a, err := h.Svc.GetThresholds(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, a)
}

func (h Handler) PutThresholds(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body domain.AlertThresholds
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	a, err := h.Svc.PutThresholds(r.Context(), claims.BranchID, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, a)
}

func (h Handler) EventsCatalog(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	response.JSON(w, http.StatusOK, events.Catalog())
}
