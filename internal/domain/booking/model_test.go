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
