package notification

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func mapNotification(n *domain.Notification) map[string]any {
	m := map[string]any{
		"id": n.ID, "branch_id": n.BranchID, "recipient_user_id": n.RecipientUserID,
		"kind": n.Kind, "severity": n.Severity, "title": n.Title, "body": n.Body,
		"entity_type": n.EntityType, "group_key": n.GroupKey,
		"occurrence_count": n.OccurrenceCount, "status": n.Status,
		"href_hint": n.HrefHint, "meta": json.RawMessage(n.MetaJSON),
		"created_at": n.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": n.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if n.EntityID != nil {
		m["entity_id"] = *n.EntityID
	}
	if n.AcknowledgedAt != nil {
		m["acknowledged_at"] = n.AcknowledgedAt.UTC().Format(time.RFC3339Nano)
	}
	if n.ResolvedAt != nil {
		m["resolved_at"] = n.ResolvedAt.UTC().Format(time.RFC3339Nano)
	}
	return m
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	q := r.URL.Query()
	var status domain.Status
	if s := q.Get("status"); s != "" {
		status = domain.Status(s)
		if !domain.ValidStatus(status) {
			response.Error(w, shared.NewValidation("invalid status"))
			return
		}
	}
	includeResolved := q.Get("include_resolved") == "1" || q.Get("include_resolved") == "true"
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	items, total, err := h.Svc.List(r.Context(), claims.UserID, status, includeResolved, limit, offset)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapNotification(&items[i]))
	}
	response.JSONMeta(w, http.StatusOK, out, map[string]any{"total": total})
}

func (h Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	n, err := h.Svc.UnreadCount(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"count": n})
}

func (h Handler) Acknowledge(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.Svc.Acknowledge(r.Context(), id, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapNotification(n))
}

func (h Handler) Resolve(w http.ResponseWriter, r *http.Request) {
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
	n, err := h.Svc.Resolve(r.Context(), id, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapNotification(n))
}

func (h Handler) AcknowledgeAll(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	n, err := h.Svc.AcknowledgeAll(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"acknowledged": n})
}

func (h Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	p, err := h.Svc.GetPreferences(r.Context(), claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"user_id": p.UserID, "email_enabled": p.EmailEnabled, "push_enabled": p.PushEnabled,
		"in_app_mandatory": true,
		"updated_at":       p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func (h Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		EmailEnabled bool `json:"email_enabled"`
		PushEnabled  bool `json:"push_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	p, err := h.Svc.UpdatePreferences(r.Context(), claims.UserID, body.EmailEnabled, body.PushEnabled)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"user_id": p.UserID, "email_enabled": p.EmailEnabled, "push_enabled": p.PushEnabled,
		"in_app_mandatory": true,
		"updated_at":       p.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func (h Handler) Rules(w http.ResponseWriter, r *http.Request) {
	rules := h.Svc.Rules()
	out := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		out = append(out, map[string]any{
			"kind": rule.Kind, "severity": rule.Severity,
			"escalate_after_seconds": int(rule.EscalateAfter.Seconds()),
			"escalate_to_roles":      rule.EscalateToRoles,
			"groupable":             rule.Groupable,
			"default_title":         rule.DefaultTitle,
			"default_href":          rule.DefaultHref,
			"entity_type":           rule.EntityType,
		})
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) ProcessEscalations(w http.ResponseWriter, r *http.Request) {
	n, err := h.Svc.ProcessEscalations(r.Context(), time.Now().UTC())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"escalated": n})
}

// Emit is a manager/dev helper to seed an alert (also used by smoke tests).
func (h Handler) Emit(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		RecipientUserID *uuid.UUID `json:"recipient_user_id"`
		Kind            string     `json:"kind"`
		Title           string     `json:"title"`
		Body            string     `json:"body"`
		EntityType      string     `json:"entity_type"`
		EntityID        *uuid.UUID `json:"entity_id"`
		HrefHint        string     `json:"href_hint"`
		Severity        string     `json:"severity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	recipient := claims.UserID
	if body.RecipientUserID != nil {
		recipient = *body.RecipientUserID
	}
	n, err := h.Svc.Emit(r.Context(), appsvc.EmitInput{
		BranchID: claims.BranchID, RecipientUserID: recipient,
		Kind: body.Kind, Title: body.Title, Body: body.Body,
		EntityType: body.EntityType, EntityID: body.EntityID, HrefHint: body.HrefHint,
		Severity: domain.Severity(body.Severity),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapNotification(n))
}
