package revenuetarget

import (
	"context"

	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

// Reactor recomputes branch revenue targets when payments/bookings change.
type Reactor struct {
	svc      *Service
	bookings bookingdomain.Repository
}

func NewReactor(svc *Service, bookings bookingdomain.Repository) *Reactor {
	return &Reactor{svc: svc, bookings: bookings}
}

func (r *Reactor) Register(bus *events.Bus) {
	bus.Subscribe(events.PaymentRecorded, r.onPaymentRecorded)
	bus.Subscribe(events.BookingConfirmed, r.onBookingConfirmed)
}

func (r *Reactor) onPaymentRecorded(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(*paymentdomain.Payment)
	if !ok || p == nil {
		return nil
	}
	b, err := r.bookings.FindByID(ctx, p.BookingID)
	if err != nil || b == nil {
		return err
	}
	_, err = r.svc.RecomputeBranch(ctx, b.BranchID)
	return err
}

func (r *Reactor) onBookingConfirmed(ctx context.Context, ev events.Event) error {
	b, ok := ev.Payload.(*bookingdomain.Booking)
	if !ok || b == nil {
		return nil
	}
	_, err := r.svc.RecomputeBranch(ctx, b.BranchID)
	return err
}
