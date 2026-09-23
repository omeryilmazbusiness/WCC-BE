package booking_test

import (
	"errors"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestBookingConfirmAndBalance(t *testing.T) {
	b := &booking.Booking{Status: booking.StatusDraft, TotalAmount: 1000, CollectedAmt: 250}
	b.RecomputeBalance()
	if b.BalanceAmt != 750 {
		t.Fatalf("balance=%d want 750", b.BalanceAmt)
	}
	if err := b.TransitionTo(booking.StatusConfirmed); err != nil {
		t.Fatal(err)
	}
	if err := b.TransitionTo(booking.StatusDraft); err == nil {
		t.Fatal("expected invalid confirmed→draft")
	} else {
		var app *shared.AppError
		if !errors.As(err, &app) || !errors.Is(app.Err, shared.ErrInvalidState) {
			t.Fatalf("want invalid state, got %v", err)
		}
	}
}

func TestApplyUpdateOnlyDraft(t *testing.T) {
	b := &booking.Booking{Status: booking.StatusDraft, TotalAmount: 100, CollectedAmt: 0}
	disc := int64(0)
	notes := "n"
	if err := b.ApplyUpdate(3, 300, "USD", &disc, &notes); err != nil {
		t.Fatal(err)
	}
	if b.PaxCount != 3 || b.BalanceAmt != 300 || b.Notes != "n" {
		t.Fatalf("unexpected update %#v", b)
	}
	b.Status = booking.StatusConfirmed
	if err := b.ApplyUpdate(1, 100, "USD", nil, nil); err == nil {
		t.Fatal("confirmed bookings must not accept update")
	}
}

func TestRecalculateFromLines(t *testing.T) {
	b := &booking.Booking{Status: booking.StatusDraft, DiscountAmt: 100}
	b.RecalculateFromLines([]booking.LineItem{
		{Quantity: 2, UnitPrice: 500, UnitCost: 300},
		{Quantity: 1, UnitPrice: 200, UnitCost: 50},
	})
	if b.TotalAmount != 1100 || b.CostAmt != 650 || b.Margin() != 350 {
		t.Fatalf("total=%d cost=%d margin=%d", b.TotalAmount, b.CostAmt, b.Margin())
	}
}

func TestCanTransition(t *testing.T) {
	if !booking.CanTransition(booking.StatusDraft, booking.StatusConfirmed) {
		t.Fatal("draft→confirmed")
	}
	if booking.CanTransition(booking.StatusCancelled, booking.StatusConfirmed) {
		t.Fatal("cancelled is terminal")
	}
}
