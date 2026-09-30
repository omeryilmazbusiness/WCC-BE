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
