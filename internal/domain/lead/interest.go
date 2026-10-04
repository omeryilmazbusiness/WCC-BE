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
	MaxChildren            = 20
	MaxChildAge            = 17
	MaxFlexDays            = 30
	maxTravelWindowLen     = 80
	maxPackageInterestLen  = 200
	maxPlaceLen            = 80
	travelDateMaxYearsPast = 1
	travelDateMaxYearsNext = 3
)

var (
	currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
	iataCode     = regexp.MustCompile(`^[A-Za-z]{3}$`)
)

// Services a lead can ask for, in display order.
var Services = []string{"flight", "hotel", "tour", "visa", "transfer", "other"}

// CabinClasses are the flight cabins, cheapest first.
var CabinClasses = []string{"economy", "premium_economy", "business", "first"}

// BoardTypes are the hotel meal plans, lightest first.
var BoardTypes = []string{"room_only", "bed_breakfast", "half_board", "full_board", "all_inclusive", "ultra_all_inclusive"}

// Preferences are the special requests and constraints a quote must respect.
var Preferences = []string{"direct_flight", "extra_baggage", "use_miles", "seat_selection", "special_meal", "accessibility"}

// TripInterest is what the customer asked for. Every part is optional.
type TripInterest struct {
	Services []string // subset of Services, canonical order
	// Origin and Destination are IATA codes (upper case) for flights, else free text.
	Origin      string
	Destination string
	TravelDate  *time.Time // departure, calendar date at UTC midnight
	ReturnDate  *time.Time // calendar date at UTC midnight, not before TravelDate
	FlexDays    int        // ± days the dates may move; 0 = exact
	// TravelWindow is used when there is no exact date, e.g. "late March".
	TravelWindow string
	Adults       int
	ChildAges    []int // one entry per child, 0..17
	Infants      int
	// PaxCount is the party size: adults + children + infants when a breakdown
	// is given, else the count the caller sent.
	PaxCount        *int
	CabinClass      string
	BoardType       string
	Preferences     []string // subset of Preferences, canonical order
	BudgetAmount    *int64   // minor units of BudgetCurrency
	BudgetCurrency  string
	PackageID       *uuid.UUID
	PackageInterest string // free text when no catalogue package matches
}

// HasService reports whether the customer asked for service s.
func (t TripInterest) HasService(s string) bool {
	for _, x := range t.Services {
		if x == s {
			return true
		}
	}
	return false
}

// Normalize trims and canonicalises the interest and validates it against
// now; field problems come back together in the error details.
func (t TripInterest) Normalize(now time.Time) (TripInterest, error) {
	t.TravelWindow = strings.TrimSpace(t.TravelWindow)
	t.PackageInterest = strings.TrimSpace(t.PackageInterest)
	t.BudgetCurrency = strings.ToUpper(strings.TrimSpace(t.BudgetCurrency))
	t.Origin = strings.Join(strings.Fields(t.Origin), " ")
	t.Destination = strings.Join(strings.Fields(t.Destination), " ")
	t.CabinClass = strings.ToLower(strings.TrimSpace(t.CabinClass))
	t.BoardType = strings.ToLower(strings.TrimSpace(t.BoardType))

	fields := map[string]any{}
	var ok bool
	if t.Services, ok = canonical(t.Services, Services); !ok {
		fields["services"] = "unknown service"
	}
	if t.Preferences, ok = canonical(t.Preferences, Preferences); !ok {
		fields["preferences"] = "unknown preference"
	}
	if t.HasService("flight") {
		t.Origin, t.Destination = upperIATA(t.Origin), upperIATA(t.Destination)
	}
	for key, place := range map[string]string{"origin": t.Origin, "destination": t.Destination} {
		if utf8.RuneCountInString(place) > maxPlaceLen {
			fields[key] = "too long"
		}
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	inRange := func(d time.Time) bool {
		return !d.Before(today.AddDate(-travelDateMaxYearsPast, 0, 0)) && !d.After(today.AddDate(travelDateMaxYearsNext, 0, 0))
	}
	if t.TravelDate != nil {
		d := dateOnly(*t.TravelDate)
		if !inRange(d) {
			fields["travel_date"] = "out of range"
		}
		t.TravelDate = &d
	}
	if t.ReturnDate != nil {
		d := dateOnly(*t.ReturnDate)
		switch {
		case !inRange(d):
			fields["return_date"] = "out of range"
		case t.TravelDate != nil && d.Before(*t.TravelDate):
			fields["return_date"] = "must not be before the departure"
		}
		t.ReturnDate = &d
	}
	if t.FlexDays < 0 || t.FlexDays > MaxFlexDays {
		fields["flex_days"] = "must be between 0 and 30"
	}
	if utf8.RuneCountInString(t.TravelWindow) > maxTravelWindowLen {
		fields["travel_window"] = "too long"
	}

	if t.Adults < 0 || t.Infants < 0 {
		fields["adults"] = "must not be negative"
	}
	if len(t.ChildAges) > MaxChildren {
		fields["child_ages"] = "too many children"
	}
	for _, age := range t.ChildAges {
		if age < 0 || age > MaxChildAge {
			fields["child_ages"] = "ages must be between 0 and 17"
		}
	}
	if t.ChildAges == nil {
		t.ChildAges = []int{}
	}
	if party := t.Adults + len(t.ChildAges) + t.Infants; party > 0 {
		if t.Adults == 0 {
			fields["adults"] = "at least one adult travels"
		}
		if t.Infants > t.Adults {
			fields["infants"] = "one infant per adult"
		}
		t.PaxCount = &party
	}
	if t.PaxCount != nil && (*t.PaxCount < 1 || *t.PaxCount > MaxPax) {
		fields["pax_count"] = "must be between 1 and 500"
	}

	if t.CabinClass != "" && !contains(CabinClasses, t.CabinClass) {
		fields["cabin_class"] = "unknown cabin class"
	}
	if t.BoardType != "" && !contains(BoardTypes, t.BoardType) {
		fields["board_type"] = "unknown board type"
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

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func upperIATA(place string) string {
	if iataCode.MatchString(place) {
		return strings.ToUpper(place)
	}
	return place
}

// canonical dedupes values into the order of allowed; ok is false on an unknown value.
func canonical(values, allowed []string) ([]string, bool) {
	seen := map[string]bool{}
	for _, v := range values {
		if k := strings.ToLower(strings.TrimSpace(v)); k != "" {
			seen[k] = true
		}
	}
	out := []string{}
	for _, a := range allowed {
		if seen[a] {
			out = append(out, a)
			delete(seen, a)
		}
	}
	return out, len(seen) == 0
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
