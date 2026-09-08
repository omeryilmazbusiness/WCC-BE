package booking

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID    uuid.UUID
	CustomerID  uuid.UUID
	DepartureID uuid.UUID
	LeadID      *uuid.UUID
	PaxCount    int
	TotalAmount int64
	Currency    string
	OwnerID     uuid.UUID
}

type Service struct {
	repo domain.Repository
	tx   *tx.Manager
	bus  *events.Bus
}

func NewService(repo domain.Repository, txm *tx.Manager, bus *events.Bus) *Service {
	return &Service{repo: repo, tx: txm, bus: bus}
}

func (s *Service) CreateDraft(ctx context.Context, in CreateInput) (*domain.Booking, error) {
	if in.PaxCount <= 0 {
		return nil, shared.NewValidation("pax_count must be > 0")
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	now := time.Now().UTC()
	b := &domain.Booking{
		ID:          uuid.New(),
		BranchID:    in.BranchID,
		CustomerID:  in.CustomerID,
		DepartureID: in.DepartureID,
		LeadID:      in.LeadID,
		Status:      domain.StatusDraft,
		PaxCount:    in.PaxCount,
		TotalAmount: in.TotalAmount,
		Currency:    in.Currency,
		OwnerID:     in.OwnerID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	b.RecomputeBalance()

	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.Create(ctx, b)
	}); err != nil {
		return nil, err
	}

	s.bus.Publish(ctx, events.Event{Name: events.BookingDrafted, Payload: b})
	return b, nil
}

func (s *Service) Confirm(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if err := b.TransitionTo(domain.StatusConfirmed); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Publish after commit so handlers see durable state.
	s.bus.Publish(ctx, events.Event{Name: events.BookingConfirmed, Payload: out})
	return out, nil
}
