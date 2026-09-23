package booking

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusConfirmed Status = "confirmed"
	StatusCancelled Status = "cancelled"
	StatusCompleted Status = "completed"
)

type Booking struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	CustomerID   uuid.UUID
	DepartureID  uuid.UUID
	LeadID       *uuid.UUID
	Status       Status
	PaxCount     int
	TotalAmount  int64
	CollectedAmt int64
	BalanceAmt   int64
	Currency     string
	OwnerID      uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Participant struct {
	ID          uuid.UUID
	BookingID   uuid.UUID
	FullName    string
	PassportNo  string
	Nationality string
	DateOfBirth *time.Time
	CreatedAt   time.Time
}

var allowed = map[Status][]Status{
	StatusDraft:     {StatusConfirmed, StatusCancelled},
	StatusConfirmed: {StatusCompleted, StatusCancelled},
	StatusCancelled: {},
	StatusCompleted: {},
}

func CanTransition(from, to Status) bool {
	for _, s := range allowed[from] {
		if s == to {
			return true
		}
	}
	return false
}

func (b *Booking) TransitionTo(to Status) error {
	if !CanTransition(b.Status, to) {
		return shared.NewInvalidState("cannot transition booking from " + string(b.Status) + " to " + string(to))
	}
	b.Status = to
	b.UpdatedAt = time.Now().UTC()
	return nil
}

func (b *Booking) RecomputeBalance() {
	b.BalanceAmt = b.TotalAmount - b.CollectedAmt
	if b.BalanceAmt < 0 {
		b.BalanceAmt = 0
	}
}

func (b *Booking) ApplyUpdate(pax int, total int64, currency string) error {
	if b.Status != StatusDraft {
		return shared.NewInvalidState("only draft bookings can be updated")
	}
	if pax <= 0 {
		return shared.NewValidation("pax_count must be > 0")
	}
	if total < 0 {
		return shared.NewValidation("total_amount must be >= 0")
	}
	b.PaxCount = pax
	b.TotalAmount = total
	if currency != "" {
		b.Currency = currency
	}
	b.RecomputeBalance()
	b.UpdatedAt = time.Now().UTC()
	return nil
}

type Repository interface {
	Create(ctx context.Context, b *Booking) error
	Update(ctx context.Context, b *Booking) error
	FindByID(ctx context.Context, id uuid.UUID) (*Booking, error)
	AddParticipant(ctx context.Context, p *Participant) error
	ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]Participant, error)
	CountConfirmedPaxByDeparture(ctx context.Context, departureID uuid.UUID) (int, error)
	ListByDeparture(ctx context.Context, departureID uuid.UUID) ([]Booking, error)
}
