package lead

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// interestRequest carries budget_amount in minor units and travel_date as
// YYYY-MM-DD.
type interestRequest struct {
	TravelDate      *string    `json:"travel_date"`
	TravelWindow    string     `json:"travel_window"`
	PaxCount        *int       `json:"pax_count"`
	BudgetAmount    *int64     `json:"budget_amount"`
	BudgetCurrency  string     `json:"budget_currency"`
	PackageID       *uuid.UUID `json:"package_id"`
	PackageInterest string     `json:"package_interest"`
}

func (r *interestRequest) toDomain() (domain.TripInterest, error) {
	if r == nil {
		return domain.TripInterest{}, nil
	}
	out := domain.TripInterest{
		TravelWindow: r.TravelWindow, PaxCount: r.PaxCount,
		BudgetAmount: r.BudgetAmount, BudgetCurrency: r.BudgetCurrency,
		PackageID: r.PackageID, PackageInterest: r.PackageInterest,
	}
	if r.TravelDate != nil && strings.TrimSpace(*r.TravelDate) != "" {
		d, err := time.Parse(time.DateOnly, strings.TrimSpace(*r.TravelDate))
		if err != nil {
			return out, shared.NewValidation("travel_date must be YYYY-MM-DD")
		}
		out.TravelDate = &d
	}
	return out, nil
}

func mapInterest(t domain.TripInterest) map[string]any {
	var date any
	if t.TravelDate != nil {
		date = t.TravelDate.Format(time.DateOnly)
	}
	return map[string]any{
		"travel_date":      date,
		"travel_window":    t.TravelWindow,
		"pax_count":        t.PaxCount,
		"budget_amount":    t.BudgetAmount,
		"budget_currency":  t.BudgetCurrency,
		"package_id":       t.PackageID,
		"package_interest": t.PackageInterest,
	}
}

type updateRequest struct {
	FullName *string          `json:"full_name"`
	Phone    *string          `json:"phone"`
	Notes    *string          `json:"notes"`
	Interest *interestRequest `json:"interest"`
}

// Update handles PATCH /leads/{id}.
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
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	in := appsvc.UpdateDetailsInput{
		LeadID: id, FullName: req.FullName, Phone: req.Phone, Notes: req.Notes,
		ActorID: claims.UserID, IP: r.RemoteAddr, UserAgent: r.UserAgent(),
	}
	if req.Interest != nil {
		interest, err := req.Interest.toDomain()
		if err != nil {
			response.Error(w, err)
			return
		}
		in.Interest = &interest
	}
	l, err := h.Svc.UpdateDetails(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapLead(l))
}
