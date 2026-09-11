package task

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	leaddomain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Reactor closes / cancels work items from domain events (SRP separate from Seeder).
// All side effects are idempotent.
type Reactor struct {
	tasks    domain.Repository
	bookings bookingdomain.Repository
	tx       tx.Runner
}

func NewReactor(tasks domain.Repository, bookings bookingdomain.Repository, txm tx.Runner) *Reactor {
	return &Reactor{tasks: tasks, bookings: bookings, tx: txm}
}

func (r *Reactor) Register(bus *events.Bus) {
	bus.Subscribe(events.PaymentRecorded, r.onPaymentRecorded)
	bus.Subscribe(events.BookingCancelled, r.onBookingCancelled)
	bus.Subscribe(events.LeadConverted, r.onLeadConverted)
}

func (r *Reactor) onPaymentRecorded(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(*paymentdomain.Payment)
	if !ok || p == nil {
		return nil
	}
	b, err := r.bookings.FindByID(ctx, p.BookingID)
	if err != nil {
		return err
	}
	// Only close payment work when balance is cleared.
	if b.BalanceAmt > 0 {
		return nil
	}
	return r.completeOpenKind(ctx, "booking", p.BookingID, domain.KindPayment)
}

func (r *Reactor) onBookingCancelled(ctx context.Context, ev events.Event) error {
	b, ok := ev.Payload.(*bookingdomain.Booking)
	if !ok || b == nil {
		return nil
	}
	return r.cancelOpenRelated(ctx, "booking", b.ID)
}

func (r *Reactor) onLeadConverted(ctx context.Context, ev events.Event) error {
	l, ok := ev.Payload.(*leaddomain.Lead)
	if !ok || l == nil {
		return nil
	}
	now := time.Now().UTC()
	due := now.Add(24 * time.Hour)
	t := domain.Task{
		ID:             uuid.New(),
		BranchID:       l.BranchID,
		Title:          "Create booking from won lead",
		Kind:           domain.KindCustom,
		Status:         domain.StatusOpen,
		AssigneeID:     l.OwnerID,
		RelatedType:    "lead",
		RelatedID:      l.ID,
		DueAt:          &due,
		IdempotencyKey: fmt.Sprintf("lead:%s:converted-booking", l.ID),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if existing, err := r.tasks.FindByIdempotencyKey(ctx, t.IdempotencyKey); err == nil && existing != nil {
		return nil
	}
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing, err := r.tasks.FindByIdempotencyKey(ctx, t.IdempotencyKey); err == nil && existing != nil {
			return nil
		}
		return r.tasks.Create(ctx, &t)
	})
}

func (r *Reactor) completeOpenKind(ctx context.Context, relatedType string, relatedID uuid.UUID, kind domain.Kind) error {
	items, err := r.tasks.ListByRelated(ctx, relatedType, relatedID)
	if err != nil {
		return err
	}
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		for i := range items {
			t := items[i]
			if t.Kind != kind {
				continue
			}
			if t.Status == domain.StatusDone || t.Status == domain.StatusCancelled {
				continue
			}
			if err := t.TransitionTo(domain.StatusDone); err != nil {
				return err
			}
			if err := r.tasks.Update(ctx, &t); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Reactor) cancelOpenRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) error {
	items, err := r.tasks.ListByRelated(ctx, relatedType, relatedID)
	if err != nil {
		return err
	}
	return r.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		for i := range items {
			t := items[i]
			if t.Status == domain.StatusDone || t.Status == domain.StatusCancelled {
				continue
			}
			if err := t.TransitionTo(domain.StatusCancelled); err != nil {
				return err
			}
			if err := r.tasks.Update(ctx, &t); err != nil {
				return err
			}
		}
		return nil
	})
}
