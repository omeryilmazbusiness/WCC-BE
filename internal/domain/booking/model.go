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
	ID            uuid.UUID
	BranchID      uuid.UUID
	CustomerID    uuid.UUID
	DepartureID   uuid.UUID
	LeadID        *uuid.UUID
	Status        Status
	PaxCount      int
	TotalAmount   int64
	CollectedAmt  int64
	BalanceAmt    int64 // TotalAmount - CollectedAmt (recomputed on payment)
	Currency      string
	OwnerID       uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Participant struct {
	ID           uuid.UUID
	BookingID    uuid.UUID
	FullName     string
	PassportNo   string
	Nationality  string
	DateOfBirth  *time.Time
	CreatedAt    time.Time
}

var allowed = map[Status][]Status{
	StatusDraft:     {StatusConfirmed, StatusCancelled},
	StatusConfirmed: {StatusCompleted, StatusCancelled},
	StatusCancelled: {},
	StatusCompleted: {},
}

func (b *Booking) TransitionTo(to Status) error {
	for _, s := range allowed[b.Status] {
		if s == to {
			b.Status = to
			b.UpdatedAt = time.Now().UTC()
			return nil
		}
	}
	return shared.NewInvalidState("cannot transition booking from " + string(b.Status) + " to " + string(to))
}

// RecomputeBalance keeps monetary fields consistent (ACID-friendly single writer).
func (b *Booking) RecomputeBalance() {
	b.BalanceAmt = b.TotalAmount - b.CollectedAmt
	if b.BalanceAmt < 0 {
		b.BalanceAmt = 0
	}
}

type Repository interface {
	Create(ctx context.Context, b *Booking) error
	Update(ctx context.Context, b *Booking) error
	FindByID(ctx context.Context, id uuid.UUID) (*Booking, error)
	AddParticipant(ctx context.Context, p *Participant) error
	ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]Participant, error)
	CountConfirmedPaxByDeparture(ctx context.Context, departureID uuid.UUID) (int, error)
}
