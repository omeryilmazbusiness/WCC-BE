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

// interestRequest carries budget_amount in minor units and dates as YYYY-MM-DD.
type interestRequest struct {
	Services        []string   `json:"services"`
	Origin          string     `json:"origin"`
	Destination     string     `json:"destination"`
	TravelDate      *string    `json:"travel_date"`
	ReturnDate      *string    `json:"return_date"`
	FlexDays        int        `json:"flex_days"`
	TravelWindow    string     `json:"travel_window"`
	Adults          int        `json:"adults"`
	ChildAges       []int      `json:"child_ages"`
	Infants         int        `json:"infants"`
	PaxCount        *int       `json:"pax_count"`
	CabinClass      string     `json:"cabin_class"`
	BoardType       string     `json:"board_type"`
	Preferences     []string   `json:"preferences"`
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
		Services: r.Services, Origin: r.Origin, Destination: r.Destination,
		FlexDays: r.FlexDays, TravelWindow: r.TravelWindow,
		Adults: r.Adults, ChildAges: r.ChildAges, Infants: r.Infants, PaxCount: r.PaxCount,
		CabinClass: r.CabinClass, BoardType: r.BoardType, Preferences: r.Preferences,
		BudgetAmount: r.BudgetAmount, BudgetCurrency: r.BudgetCurrency,
		PackageID: r.PackageID, PackageInterest: r.PackageInterest,
	}
	var err error
	if out.TravelDate, err = optionalDate(r.TravelDate, "travel_date"); err != nil {
		return out, err
	}
	if out.ReturnDate, err = optionalDate(r.ReturnDate, "return_date"); err != nil {
		return out, err
	}
	return out, nil
}

func optionalDate(raw *string, key string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	d, err := time.Parse(time.DateOnly, strings.TrimSpace(*raw))
	if err != nil {
		return nil, shared.NewValidation(key + " must be YYYY-MM-DD")
	}
	return &d, nil
}

func formatDate(d *time.Time) any {
	if d == nil {
		return nil
	}
	return d.Format(time.DateOnly)
}

func mapInterest(t domain.TripInterest) map[string]any {
	return map[string]any{
		"services":         nonNil(t.Services),
		"origin":           t.Origin,
		"destination":      t.Destination,
		"travel_date":      formatDate(t.TravelDate),
		"return_date":      formatDate(t.ReturnDate),
		"flex_days":        t.FlexDays,
		"travel_window":    t.TravelWindow,
		"adults":           t.Adults,
		"child_ages":       nonNil(t.ChildAges),
		"infants":          t.Infants,
		"pax_count":        t.PaxCount,
		"cabin_class":      t.CabinClass,
		"board_type":       t.BoardType,
		"preferences":      nonNil(t.Preferences),
		"budget_amount":    t.BudgetAmount,
		"budget_currency":  t.BudgetCurrency,
		"package_id":       t.PackageID,
		"package_interest": t.PackageInterest,
	}
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

type updateRequest struct {
	FullName *string          `json:"full_name"`
	Phone    *string          `json:"phone"`
	Notes    *string          `json:"notes"`
	Profile  *profileRequest  `json:"profile"`
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
	if req.Profile != nil {
		profile, err := req.Profile.toDomain()
		if err != nil {
			response.Error(w, err)
			return
		}
		in.Profile = &profile
	}
	l, err := h.Svc.UpdateDetails(r.Context(), in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapLead(l))
}
