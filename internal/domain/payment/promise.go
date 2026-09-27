package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type PromiseStatus string

const (
	PromiseOpen      PromiseStatus = "open"
	PromiseKept      PromiseStatus = "kept"
	PromiseBroken    PromiseStatus = "broken"
	PromiseCancelled PromiseStatus = "cancelled"
)

// Promise is a customer's commitment to pay Amount by PromisedOn (T-274).
type Promise struct {
	ID         uuid.UUID
	BookingID  uuid.UUID
	BranchID   uuid.UUID
	Amount     int64
	Currency   string
	PromisedOn time.Time // calendar date
	Note       string
	Status     PromiseStatus
	TaskID     *uuid.UUID
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

// ValidateNew checks a promise before it is stored; today is the business
// calendar date.
func (p *Promise) ValidateNew(today time.Time) error {
	if p.Amount <= 0 {
		return shared.NewValidation("amount must be > 0")
	}
	if p.PromisedOn.IsZero() {
		return shared.NewValidation("promised_on is required")
	}
	if fx.DateOf(p.PromisedOn).Before(fx.DateOf(today)) {
		return shared.NewValidation("promised_on cannot be in the past")
	}
	return nil
}

// Outcome evaluates an open promise: kept once verified collections recorded
// since the promise was created cover its amount, broken once promised_on has
// passed without that, open otherwise. Resolved promises keep their status.
func (p *Promise) Outcome(collectedSince int64, today time.Time) PromiseStatus {
	if p.Status != PromiseOpen {
		return p.Status
	}
	if collectedSince >= p.Amount {
		return PromiseKept
	}
	if fx.DateOf(today).After(fx.DateOf(p.PromisedOn)) {
		return PromiseBroken
	}
	return PromiseOpen
}

// Resolve moves an open promise to a final status.
func (p *Promise) Resolve(to PromiseStatus, at time.Time) error {
	if p.Status != PromiseOpen {
		return shared.NewInvalidState(fmt.Sprintf("promise is already %s", p.Status))
	}
	if to == PromiseOpen {
		return shared.NewInvalidState("promise is already open")
	}
	p.Status = to
	at = at.UTC()
	p.ResolvedAt = &at
	return nil
}

// PromiseSummary aggregates the open promises of a booking.
type PromiseSummary struct {
	OpenCount      int
	OpenAmount     int64
	NextPromisedOn *time.Time
}

// SummarizePromises counts promises whose (effective) status is open.
func SummarizePromises(items []Promise) PromiseSummary {
	var s PromiseSummary
	for i := range items {
		p := items[i]
		if p.Status != PromiseOpen {
			continue
		}
		s.OpenCount++
		s.OpenAmount += p.Amount
		if s.NextPromisedOn == nil || p.PromisedOn.Before(*s.NextPromisedOn) {
			d := p.PromisedOn
			s.NextPromisedOn = &d
		}
	}
	return s
}

type PromiseRepository interface {
	InsertPromise(ctx context.Context, p *Promise) error
	GetPromise(ctx context.Context, id uuid.UUID) (*Promise, error)
	ListPromises(ctx context.Context, bookingID uuid.UUID) ([]Promise, error)
	// ListOpenPromises pages open promises by id for the resolver job.
	ListOpenPromises(ctx context.Context, afterID uuid.UUID, limit int) ([]Promise, error)
	// ResolvePromise updates an open promise; false when it was no longer open.
	ResolvePromise(ctx context.Context, p *Promise) (bool, error)
	// SumCollectedSince sums verified/approved entries in currency recorded at
	// or after since.
	SumCollectedSince(ctx context.Context, bookingID uuid.UUID, currency string, since time.Time) (int64, error)
}
