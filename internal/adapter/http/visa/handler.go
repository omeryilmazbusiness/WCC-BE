package visa

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/visa"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/visa"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func mapCase(v *domain.VisaCase) map[string]any {
	m := map[string]any{
		"id": v.ID, "branch_id": v.BranchID, "booking_id": v.BookingID,
		"status": v.Status, "external_ref": v.ExternalRef, "notes": v.Notes,
		"created_by": v.CreatedBy,
		"created_at": v.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": v.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if v.ParticipantID != nil {
		m["participant_id"] = *v.ParticipantID
	}
	if v.CustomerID != nil {
		m["customer_id"] = *v.CustomerID
	}
	if v.SubmittedAt != nil {
		m["submitted_at"] = v.SubmittedAt.UTC().Format(time.RFC3339Nano)
	}
	if v.DecidedAt != nil {
		m["decided_at"] = v.DecidedAt.UTC().Format(time.RFC3339Nano)
	}
	if v.ExpiresAt != nil {
		m["expires_at"] = v.ExpiresAt.Format("2006-01-02")
	}
	return m
}

func (h Handler) Create(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		BookingID     uuid.UUID  `json:"booking_id"`
		ParticipantID *uuid.UUID `json:"participant_id"`
		CustomerID    *uuid.UUID `json:"customer_id"`
		ExternalRef   string     `json:"external_ref"`
		Notes         string     `json:"notes"`
		ExpiresAt     *string    `json:"expires_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	var expires *time.Time
	if body.ExpiresAt != nil && *body.ExpiresAt != "" {
		t, err := time.Parse("2006-01-02", *body.ExpiresAt)
		if err != nil {
			response.Error(w, shared.NewValidation("expires_at must be YYYY-MM-DD"))
			return
		}
		expires = &t
	}
	v, err := h.Svc.Create(r.Context(), appsvc.CreateInput{
		BranchID: claims.BranchID, BookingID: body.BookingID, ParticipantID: body.ParticipantID,
		CustomerID: body.CustomerID, ExternalRef: body.ExternalRef, Notes: body.Notes,
		ExpiresAt: expires, CreatedBy: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapCase(v))
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	v, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapCase(v))
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	bookingID, err := uuid.Parse(r.URL.Query().Get("booking_id"))
	if err != nil {
		response.Error(w, shared.NewValidation("booking_id must be a uuid"))
		return
	}
	items, err := h.Svc.ListByBooking(r.Context(), bookingID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapCase(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Transition(w http.ResponseWriter, r *http.Request) {
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
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	v, err := h.Svc.Transition(r.Context(), appsvc.TransitionInput{
		VisaCaseID: id, ToStatus: domain.Status(body.Status), ActorID: claims.UserID, Note: body.Note,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapCase(v))
}
