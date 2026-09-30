package lead

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	MaxPax                 = 500
	maxTravelWindowLen     = 80
	maxPackageInterestLen  = 200
	travelDateMaxYearsPast = 1
	travelDateMaxYearsNext = 3
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// TripInterest is what the customer asked for. Every part is optional.
type TripInterest struct {
	TravelDate      *time.Time // calendar date, UTC midnight
	TravelWindow    string     // when there is no exact date, e.g. "late March"
	PaxCount        *int
	BudgetAmount    *int64 // minor units of BudgetCurrency
	BudgetCurrency  string
	PackageID       *uuid.UUID
	PackageInterest string // free text when no catalogue package matches
}

// Normalize trims and canonicalises the interest and validates it against
// now; field problems come back together in the error details.
func (t TripInterest) Normalize(now time.Time) (TripInterest, error) {
	t.TravelWindow = strings.TrimSpace(t.TravelWindow)
	t.PackageInterest = strings.TrimSpace(t.PackageInterest)
	t.BudgetCurrency = strings.ToUpper(strings.TrimSpace(t.BudgetCurrency))

	fields := map[string]any{}
	if t.TravelDate != nil {
		d := time.Date(t.TravelDate.Year(), t.TravelDate.Month(), t.TravelDate.Day(), 0, 0, 0, 0, time.UTC)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if d.Before(today.AddDate(-travelDateMaxYearsPast, 0, 0)) || d.After(today.AddDate(travelDateMaxYearsNext, 0, 0)) {
			fields["travel_date"] = "out of range"
		}
		t.TravelDate = &d
	}
	if utf8.RuneCountInString(t.TravelWindow) > maxTravelWindowLen {
		fields["travel_window"] = "too long"
	}
	if t.PaxCount != nil && (*t.PaxCount < 1 || *t.PaxCount > MaxPax) {
		fields["pax_count"] = "must be between 1 and 500"
	}
	if t.BudgetAmount != nil {
		if *t.BudgetAmount < 0 {
			fields["budget_amount"] = "must not be negative"
		}
		if t.BudgetCurrency == "" {
			fields["budget_currency"] = "required with a budget"
		}
	}
	if t.BudgetCurrency != "" && !currencyCode.MatchString(t.BudgetCurrency) {
		fields["budget_currency"] = "must be a 3-letter ISO code"
	}
	if utf8.RuneCountInString(t.PackageInterest) > maxPackageInterestLen {
		fields["package_interest"] = "too long"
	}
	if len(fields) > 0 {
		err := shared.NewValidation("invalid trip interest")
		err.Details = fields
		return t, err
	}
	return t, nil
}
