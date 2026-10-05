package booking

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid "+name))
		return uuid.Nil, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return false
	}
	return true
}

func actorID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return uuid.Nil, false
	}
	return claims.UserID, true
}

func rfc3339(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// UpdateProfile: PATCH /bookings/{id}/profile.
func (h Handler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req profileRequest
	if !decode(w, r, &req) {
		return
	}
	b, err := h.Svc.UpdateProfile(r.Context(), id, req.profile())
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b, fieldAccessFor(r)))
}

// ExtendHold: POST /bookings/{id}/hold/extend {hold_expires_at}.
func (h Handler) ExtendHold(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		HoldExpiresAt string `json:"hold_expires_at"`
	}
	if !decode(w, r, &req) {
		return
	}
	until, err := time.Parse(time.RFC3339, strings.TrimSpace(req.HoldExpiresAt))
	if err != nil {
		response.Error(w, shared.NewValidation("hold_expires_at must be RFC3339"))
		return
	}
	b, err := h.Svc.ExtendHold(r.Context(), id, until)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapBooking(b, fieldAccessFor(r)))
}

// CancellationQuote: GET /bookings/{id}/cancellation-quote.
func (h Handler) CancellationQuote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	q, err := h.Svc.CancellationQuote(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, q)
}

func mapNote(n domain.Note, actor uuid.UUID) map[string]any {
	return map[string]any{
		"id": n.ID, "booking_id": n.BookingID, "author_id": n.AuthorID, "author_name": n.AuthorName,
		"body": n.Body, "pinned": n.Pinned, "can_delete": n.CanDelete(actor),
		"created_at": n.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (h Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	actor, ok := actorID(w, r)
	if !ok {
		return
	}
	items, err := h.Svc.ListNotes(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, n := range items {
		out = append(out, mapNote(n, actor))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) AddNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	actor, ok := actorID(w, r)
	if !ok {
		return
	}
	var req struct {
		Body   string `json:"body"`
		Pinned bool   `json:"pinned"`
	}
	if !decode(w, r, &req) {
		return
	}
	n, err := h.Svc.AddNote(r.Context(), id, actor, req.Body, req.Pinned)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapNote(*n, actor))
}

func (h Handler) DeleteNote(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	noteID, ok := pathID(w, r, "noteId")
	if !ok {
		return
	}
	actor, ok := actorID(w, r)
	if !ok {
		return
	}
	if err := h.Svc.DeleteNote(r.Context(), id, noteID, actor); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mapChange(c domain.ChangeRequest) map[string]any {
	return map[string]any{
		"id": c.ID, "booking_id": c.BookingID, "kind": c.Kind, "details": c.Details, "status": c.Status,
		"requested_by": c.RequestedBy, "requested_by_name": c.RequestedByName, "resolved_by": c.ResolvedBy,
		"resolution_note": c.ResolutionNote, "created_at": c.CreatedAt.UTC().Format(time.RFC3339),
		"resolved_at": rfc3339(c.ResolvedAt),
	}
}

func (h Handler) ListChanges(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	items, err := h.Svc.ListChanges(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, c := range items {
		out = append(out, mapChange(c))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) RequestChange(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	actor, ok := actorID(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind    string `json:"kind"`
		Details string `json:"details"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := h.Svc.RequestChange(r.Context(), id, actor, req.Kind, req.Details)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapChange(*c))
}

func (h Handler) ResolveChange(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	changeID, ok := pathID(w, r, "changeId")
	if !ok {
		return
	}
	actor, ok := actorID(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := h.Svc.ResolveChange(r.Context(), id, changeID, actor, req.Status, req.Note)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapChange(*c))
}

// Activity: GET /bookings/{id}/activity?limit=.
func (h Handler) Activity(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Svc.Activity(r.Context(), id, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, a := range items {
		out = append(out, map[string]any{
			"id": a.ID, "action": a.Action, "entity_type": a.EntityType, "actor_name": a.ActorName,
			"actor_type": a.ActorType, "details": a.Details, "created_at": a.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	response.JSON(w, http.StatusOK, out)
}

// RecordShare: POST /bookings/{id}/shares {channel, document}.
func (h Handler) RecordShare(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Channel  string `json:"channel"`
		Document string `json:"document"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := h.Svc.RecordShare(r.Context(), id, req.Channel, req.Document); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// CreatePaymentLink: POST /bookings/{id}/payment-link {amount} (0 = balance).
func (h Handler) CreatePaymentLink(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var req struct {
		Amount int64 `json:"amount"`
	}
	if !decode(w, r, &req) {
		return
	}
	link, err := h.Svc.CreatePaymentLink(r.Context(), id, req.Amount)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, link)
}
