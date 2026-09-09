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
	if err := b.ApplyUpdate(3, 300, "USD"); err != nil {
		t.Fatal(err)
	}
	if b.PaxCount != 3 || b.BalanceAmt != 300 {
		t.Fatalf("unexpected update %#v", b)
	}
	b.Status = booking.StatusConfirmed
	if err := b.ApplyUpdate(1, 100, "USD"); err == nil {
		t.Fatal("confirmed bookings must not accept update")
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
