package lead

import (
	"errors"
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func ptr[T any](v T) *T { return &v }

func TestTripInterestNormalize(t *testing.T) {
	now := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	got, err := TripInterest{
		TravelDate:     ptr(time.Date(2027, 3, 10, 18, 30, 0, 0, time.FixedZone("x", 3*3600))),
		TravelWindow:   "  late March ",
		PaxCount:       ptr(4),
		BudgetAmount:   ptr(int64(500000)),
		BudgetCurrency: " usd ",
	}.Normalize(now)
	if err != nil {
		t.Fatal(err)
	}
	if got.TravelDate.Format(time.DateOnly) != "2027-03-10" || got.TravelDate.Location() != time.UTC {
		t.Fatalf("date = %v", got.TravelDate)
	}
	if got.TravelWindow != "late March" || got.BudgetCurrency != "USD" {
		t.Fatalf("got %+v", got)
	}
}

func TestTripInterestNormalizeRejects(t *testing.T) {
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	cases := map[string]TripInterest{
		"pax_count":       {PaxCount: ptr(0)},
		"budget_currency": {BudgetAmount: ptr(int64(100))},
		"budget_amount":   {BudgetAmount: ptr(int64(-1)), BudgetCurrency: "USD"},
		"travel_date":     {TravelDate: ptr(now.AddDate(5, 0, 0))},
	}
	for field, in := range cases {
		_, err := in.Normalize(now)
		var app *shared.AppError
		if !errors.As(err, &app) {
			t.Fatalf("%s: want validation error, got %v", field, err)
		}
		if _, ok := app.Details[field]; !ok {
			t.Fatalf("%s: details = %v", field, app.Details)
		}
	}
	if _, err := (TripInterest{BudgetCurrency: "US"}).Normalize(now); err == nil {
		t.Fatal("2-letter currency accepted")
	}
}

func TestTripInterestScope(t *testing.T) {
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	got, err := TripInterest{
		Services:    []string{" Hotel", "flight", "hotel", ""},
		Origin:      " ist ",
		Destination: "jfk",
		TravelDate:  ptr(now.AddDate(0, 1, 0)),
		ReturnDate:  ptr(now.AddDate(0, 1, 7)),
		FlexDays:    3,
		Adults:      2,
		ChildAges:   []int{4, 11},
		Infants:     1,
		CabinClass:  " Business ",
		BoardType:   "all_inclusive",
		Preferences: []string{"use_miles", "direct_flight"},
	}.Normalize(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Services) != 2 || got.Services[0] != "flight" || got.Services[1] != "hotel" {
		t.Fatalf("services = %v", got.Services)
	}
	if got.Origin != "IST" || got.Destination != "JFK" {
		t.Fatalf("route = %s-%s", got.Origin, got.Destination)
	}
	if got.PaxCount == nil || *got.PaxCount != 5 {
		t.Fatalf("pax = %v", got.PaxCount)
	}
	if got.CabinClass != "business" || got.Preferences[0] != "direct_flight" {
		t.Fatalf("got %+v", got)
	}

	hotelOnly, err := TripInterest{Services: []string{"hotel"}, Destination: "ant"}.Normalize(now)
	if err != nil || hotelOnly.Destination != "ant" {
		t.Fatalf("non-flight destination must stay free text: %q %v", hotelOnly.Destination, err)
	}
}

func TestTripInterestScopeRejects(t *testing.T) {
	now := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	cases := map[string]TripInterest{
		"services":    {Services: []string{"cruise"}},
		"preferences": {Preferences: []string{"pets"}},
		"return_date": {TravelDate: ptr(now.AddDate(0, 1, 0)), ReturnDate: ptr(now)},
		"flex_days":   {FlexDays: 31},
		"child_ages":  {Adults: 1, ChildAges: []int{18}},
		"adults":      {ChildAges: []int{5}},
		"infants":     {Adults: 1, Infants: 2},
		"cabin_class": {CabinClass: "luxury"},
		"board_type":  {BoardType: "buffet"},
	}
	for field, in := range cases {
		_, err := in.Normalize(now)
		var app *shared.AppError
		if !errors.As(err, &app) {
			t.Fatalf("%s: want validation error, got %v", field, err)
		}
		if _, ok := app.Details[field]; !ok {
			t.Fatalf("%s: details = %v", field, app.Details)
		}
	}
}
