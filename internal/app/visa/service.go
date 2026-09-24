package visa

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/visa"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID      uuid.UUID
	BookingID     uuid.UUID
	ParticipantID *uuid.UUID
	CustomerID    *uuid.UUID
	ExternalRef   string
	Notes         string
	ExpiresAt     *time.Time
	CreatedBy     uuid.UUID
}

type TransitionInput struct {
	VisaCaseID uuid.UUID
	ToStatus   domain.Status
	ActorID    uuid.UUID
	Note       string
}

type Service struct {
	repo domain.Repository
	tx   tx.Runner
}

func NewService(repo domain.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, tx: txm}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.VisaCase, error) {
	if in.BranchID == uuid.Nil || in.BookingID == uuid.Nil || in.CreatedBy == uuid.Nil {
		return nil, shared.NewValidation("branch_id, booking_id and created_by are required")
	}
	now := time.Now().UTC()
	v := &domain.VisaCase{
		ID: uuid.New(), BranchID: in.BranchID, BookingID: in.BookingID,
		ParticipantID: in.ParticipantID, CustomerID: in.CustomerID,
		Status: domain.StatusDraft, ExternalRef: strings.TrimSpace(in.ExternalRef),
		Notes: strings.TrimSpace(in.Notes), ExpiresAt: in.ExpiresAt,
		CreatedBy: in.CreatedBy, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.VisaCase, error) {
	v, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("visa_case")
	}
	return v, nil
}

func (s *Service) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.VisaCase, error) {
	if bookingID == uuid.Nil {
		return nil, shared.NewValidation("booking_id is required")
	}
	return s.repo.ListByBooking(ctx, bookingID)
}

func (s *Service) Transition(ctx context.Context, in TransitionInput) (*domain.VisaCase, error) {
	if !domain.ValidStatus(in.ToStatus) {
		return nil, shared.NewValidation("invalid status")
	}
	var out *domain.VisaCase
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		v, err := s.repo.FindByID(ctx, in.VisaCaseID)
		if err != nil {
			return shared.NewNotFound("visa_case")
		}
		ev, err := v.TransitionTo(in.ToStatus, in.ActorID, in.Note)
		if err != nil {
			return err
		}
		if err := s.repo.Update(ctx, v); err != nil {
			return err
		}
		if err := s.repo.AppendEvent(ctx, ev); err != nil {
			return err
		}
		out = v
		return nil
	})
	return out, err
}

func (s *Service) ListEvents(ctx context.Context, visaCaseID uuid.UUID) ([]domain.Event, error) {
	return s.repo.ListEvents(ctx, visaCaseID)
}
