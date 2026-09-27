package audithttp

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Service is the audit read/export port the handler needs (ISP).
type Service interface {
	List(ctx context.Context, f domain.ListFilter) ([]domain.Event, int64, error)
	Actions(ctx context.Context) ([]string, error)
	Export(ctx context.Context, f domain.ListFilter, fn func(domain.Event) error) error
}

type Handler struct {
	Svc Service
}

// EventDTO is the public audit event contract.
type EventDTO struct {
	ID         uuid.UUID       `json:"id"`
	ActorID    *uuid.UUID      `json:"actor_id"`
	ActorName  string          `json:"actor_name"`
	ActorType  string          `json:"actor_type"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   *uuid.UUID      `json:"entity_id"`
	BranchID   *uuid.UUID      `json:"branch_id"`
	Before     json.RawMessage `json:"before"`
	After      json.RawMessage `json:"after"`
	Extra      json.RawMessage `json:"extra"`
	IP         string          `json:"ip"`
	UserAgent  string          `json:"user_agent"`
	SessionID  *uuid.UUID      `json:"session_id"`
	RequestID  string          `json:"request_id"`
	CreatedAt  string          `json:"created_at"`
}

func ToDTO(e domain.Event) EventDTO {
	return EventDTO{
		ID: e.ID, ActorID: e.ActorID, ActorName: e.ActorName, ActorType: string(e.ActorType),
		Action: e.Action, EntityType: e.EntityType, EntityID: e.EntityID, BranchID: e.BranchID,
		Before: jsonOrNull(e.Before), After: jsonOrNull(e.After), Extra: extraObject(e.Metadata),
		IP: e.IP, UserAgent: e.UserAgent, SessionID: e.SessionID, RequestID: e.RequestID,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	page := request.Page(r)
	f, err := parseFilter(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	f.Limit, f.Offset = page.Limit, page.Offset

	items, total, err := h.Svc.List(r.Context(), f)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]EventDTO, 0, len(items))
	for _, e := range items {
		out = append(out, ToDTO(e))
	}
	meta := shared.NewPageMeta(total, page)
	writeData(w, out, map[string]any{
		"total": meta.Total, "limit": meta.Limit, "offset": meta.Offset, "page": meta.Page, "total_pages": meta.TotalPages,
	})
}

// Actions returns the distinct actions for the filter dropdown.
func (h Handler) Actions(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.Actions(r.Context())
	if err != nil {
		response.Error(w, err)
		return
	}
	if items == nil {
		items = []string{}
	}
	writeData(w, items, nil)
}

var csvHeader = []string{
	"created_at", "actor_name", "actor_type", "action", "entity_type", "entity_id", "branch_id",
	"ip", "session_id", "request_id", "before", "after", "extra",
}

// ExportCSV streams up to domain.ExportMaxRows events as CSV. Errors before
// the first row are returned as JSON; later failures truncate the stream.
func (h Handler) ExportCSV(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	f, err := parseFilter(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	cw := csv.NewWriter(w)
	started := false
	start := func() error {
		started = true
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition",
			`attachment; filename="audit-events-`+time.Now().UTC().Format("20060102-150405")+`.csv"`)
		w.WriteHeader(http.StatusOK)
		return cw.Write(csvHeader)
	}
	err = h.Svc.Export(r.Context(), f, func(e domain.Event) error {
		if !started {
			if err := start(); err != nil {
				return err
			}
		}
		return cw.Write(csvRow(e))
	})
	switch {
	case err != nil && !started:
		response.Error(w, err)
		return
	case err != nil:
		slog.ErrorContext(r.Context(), "audit export aborted", "error", err)
	case !started:
		_ = start()
	}
	cw.Flush()
}

func csvRow(e domain.Event) []string {
	row := []string{
		e.CreatedAt.UTC().Format(time.RFC3339), e.ActorName, string(e.ActorType), e.Action, e.EntityType,
		uuidOrEmpty(e.EntityID), uuidOrEmpty(e.BranchID), e.IP, uuidOrEmpty(e.SessionID), e.RequestID,
		string(e.Before), string(e.After), string(extraObject(e.Metadata)),
	}
	for i := range row {
		row[i] = neutralizeFormula(row[i])
	}
	return row
}

// neutralizeFormula stops spreadsheet apps from evaluating a cell (CSV/formula injection).
func neutralizeFormula(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func parseFilter(r *http.Request) (domain.ListFilter, error) {
	f := domain.ListFilter{
		EntityType: request.FilterString(r, "entity_type"),
		Action:     request.FilterString(r, "action"),
	}
	var err error
	if f.ActorID, err = optionalUUID(r, "actor_id"); err != nil {
		return f, err
	}
	if f.EntityID, err = optionalUUID(r, "entity_id"); err != nil {
		return f, err
	}
	if f.From, err = optionalTime(r, "from", false); err != nil {
		return f, err
	}
	if f.To, err = optionalTime(r, "to", true); err != nil {
		return f, err
	}
	if f.BranchID, err = request.Branch(r); err != nil {
		return f, err
	}
	return f, nil
}

func optionalUUID(r *http.Request, key string) (*uuid.UUID, error) {
	v := request.FilterString(r, key)
	if v == "" {
		return nil, nil
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return nil, shared.NewValidation("invalid " + key)
	}
	return &id, nil
}

// optionalTime accepts RFC3339 or YYYY-MM-DD; a date-only upper bound covers
// the whole day.
func optionalTime(r *http.Request, key string, endOfDay bool) (*time.Time, error) {
	v := request.FilterString(r, key)
	if v == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t, nil
	}
	d, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, shared.NewValidation("invalid " + key + " (RFC3339 or YYYY-MM-DD)")
	}
	if endOfDay {
		d = d.Add(24*time.Hour - time.Nanosecond)
	}
	return &d, nil
}

func writeData(w http.ResponseWriter, data any, meta map[string]any) {
	body := map[string]any{"data": data}
	if meta != nil {
		body["meta"] = meta
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(body)
}

func jsonOrNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}

func extraObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}")
	}
	return raw
}

func uuidOrEmpty(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
