package booking

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	pkgdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
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

type UpdateInput struct {
	PaxCount    int
	TotalAmount int64
	Currency    string
}

type AddParticipantInput struct {
	FullName    string
	PassportNo  string
	Nationality string
	DateOfBirth *time.Time
}

type Service struct {
	repo       domain.Repository
	departures pkgdomain.Repository
	tx         *tx.Manager
	bus        *events.Bus
	audit      audit.Recorder
}

func NewService(
	repo domain.Repository,
	departures pkgdomain.Repository,
	txm *tx.Manager,
	bus *events.Bus,
) *Service {
	return &Service{repo: repo, departures: departures, tx: txm, bus: bus}
}

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

func (s *Service) CreateDraft(ctx context.Context, in CreateInput) (*domain.Booking, error) {
	if in.PaxCount <= 0 {
		return nil, shared.NewValidation("pax_count must be > 0")
	}
	if in.CustomerID == uuid.Nil || in.DepartureID == uuid.Nil {
		return nil, shared.NewValidation("customer_id and departure_id are required")
	}
	if _, err := s.departures.FindDeparture(ctx, in.DepartureID); err != nil {
		return nil, shared.NewNotFound("departure")
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

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	b, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return b, nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*domain.Booking, error) {
	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if err := b.ApplyUpdate(in.PaxCount, in.TotalAmount, in.Currency); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}

func (s *Service) Confirm(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		dep, err := s.departures.FindDeparture(ctx, b.DepartureID)
		if err != nil {
			return shared.NewNotFound("departure")
		}
		soldBefore, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
		if err != nil {
			return err
		}
		check := *dep
		check.CapacitySold = soldBefore
		if !check.CanSell(b.PaxCount) {
			return shared.NewConflict("departure capacity exceeded")
		}
		if err := b.TransitionTo(domain.StatusConfirmed); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		soldAfter, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
		if err != nil {
			return err
		}
		if err := s.departures.UpdateDepartureCapacitySold(ctx, b.DepartureID, soldAfter); err != nil {
			return err
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.audit != nil && out != nil {
		id := out.ID
		_ = s.audit.Record(ctx, audit.RecordInput{
			ActorID: out.OwnerID, Action: "booking.status_changed", EntityType: "booking", EntityID: &id, BranchID: &out.BranchID,
			After: map[string]any{"status": out.Status},
		})
	}
	s.bus.Publish(ctx, events.Event{Name: events.BookingConfirmed, Payload: out})
	return out, nil
}

func (s *Service) Transition(ctx context.Context, bookingID uuid.UUID, to domain.Status) (*domain.Booking, error) {
	switch to {
	case domain.StatusConfirmed:
		return s.Confirm(ctx, bookingID)
	case domain.StatusCancelled:
		return s.Cancel(ctx, bookingID)
	case domain.StatusCompleted:
		return s.complete(ctx, bookingID)
	default:
		return nil, shared.NewValidation("unsupported status")
	}
}

func (s *Service) Cancel(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	var out *domain.Booking
	var wasConfirmed bool
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		wasConfirmed = b.Status == domain.StatusConfirmed
		if err := b.TransitionTo(domain.StatusCancelled); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		if wasConfirmed {
			sold, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
			if err != nil {
				return err
			}
			if err := s.departures.UpdateDepartureCapacitySold(ctx, b.DepartureID, sold); err != nil {
				return err
			}
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.audit != nil && out != nil {
		id := out.ID
		_ = s.audit.Record(ctx, audit.RecordInput{
			ActorID: out.OwnerID, Action: "booking.status_changed", EntityType: "booking", EntityID: &id, BranchID: &out.BranchID,
			After: map[string]any{"status": out.Status},
		})
	}
	s.bus.Publish(ctx, events.Event{Name: events.BookingCancelled, Payload: out})
	return out, nil
}

func (s *Service) complete(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if err := b.TransitionTo(domain.StatusCompleted); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}

func (s *Service) AddParticipant(ctx context.Context, bookingID uuid.UUID, in AddParticipantInput) (*domain.Participant, error) {
	name := strings.TrimSpace(in.FullName)
	if name == "" {
		return nil, shared.NewValidation("full_name is required")
	}
	var out *domain.Participant
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status == domain.StatusCancelled {
			return shared.NewInvalidState("cannot add participants to cancelled booking")
		}
		existing, err := s.repo.ListParticipants(ctx, bookingID)
		if err != nil {
			return err
		}
		if len(existing) >= b.PaxCount {
			return shared.NewConflict("participant count would exceed pax_count")
		}
		p := &domain.Participant{
			ID:          uuid.New(),
			BookingID:   bookingID,
			FullName:    name,
			PassportNo:  strings.TrimSpace(in.PassportNo),
			Nationality: strings.TrimSpace(in.Nationality),
			DateOfBirth: in.DateOfBirth,
			CreatedAt:   time.Now().UTC(),
		}
		if err := s.repo.AddParticipant(ctx, p); err != nil {
			return err
		}
		out = p
		return nil
	})
	return out, err
}

func (s *Service) ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]domain.Participant, error) {
	if _, err := s.repo.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return s.repo.ListParticipants(ctx, bookingID)
}
