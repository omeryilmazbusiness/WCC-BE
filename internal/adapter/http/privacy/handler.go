package privacy

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/privacy"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func actor(w http.ResponseWriter, r *http.Request) (appsvc.Actor, bool) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return appsvc.Actor{}, false
	}
	return appsvc.Actor{UserID: claims.UserID, IP: middleware.ClientIP(r), UserAgent: r.UserAgent()}, true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid "+name))
		return uuid.Nil, false
	}
	return id, true
}

// RevealCustomerPassport: POST /v1/customers/{id}/reveal-passport.
func (h Handler) RevealCustomerPassport(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	passport, err := h.Svc.RevealCustomerPassport(r.Context(), a, id)
	if err != nil {
		response.Error(w, err)
		return
	}
	noStore(w)
	response.JSON(w, http.StatusOK, map[string]string{"passport_no": passport})
}

// RevealParticipantPassport: POST /v1/bookings/{id}/participants/{participantId}/reveal-passport.
func (h Handler) RevealParticipantPassport(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	bookingID, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	participantID, ok := pathUUID(w, r, "participantId")
	if !ok {
		return
	}
	passport, err := h.Svc.RevealParticipantPassport(r.Context(), a, bookingID, participantID)
	if err != nil {
		response.Error(w, err)
		return
	}
	noStore(w)
	response.JSON(w, http.StatusOK, map[string]string{"passport_no": passport})
}

// Export: GET /v1/customers/{id}/export.
func (h Handler) Export(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	bundle, err := h.Svc.Export(r.Context(), a, id)
	if err != nil {
		response.Error(w, err)
		return
	}
	noStore(w)
	w.Header().Set("Content-Disposition", `attachment; filename="customer-`+id.String()+`-export.json"`)
	response.JSON(w, http.StatusOK, bundle)
}

// Anonymize: POST /v1/customers/{id}/anonymize {"reason": "..."}.
func (h Handler) Anonymize(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r)
	if !ok {
		return
	}
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.Anonymize(r.Context(), a, id, body.Reason)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}

func noStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}
