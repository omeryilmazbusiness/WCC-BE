package lead

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID   uuid.UUID
	CustomerID *uuid.UUID
	FullName   string
	Phone      string
	Source     string
	OwnerID    uuid.UUID
	Notes      string
	ActorID    uuid.UUID
}

type Service struct {
	repo domain.Repository
	tx   *tx.Manager
	bus  *events.Bus
}

func NewService(repo domain.Repository, txm *tx.Manager, bus *events.Bus) *Service {
	return &Service{repo: repo, tx: txm, bus: bus}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Lead, error) {
	name := strings.TrimSpace(in.FullName)
	phone := strings.TrimSpace(in.Phone)
	if name == "" || phone == "" {
		return nil, shared.NewValidation("full_name and phone are required")
	}
	now := time.Now().UTC()
	l := &domain.Lead{
		ID:         uuid.New(),
		BranchID:   in.BranchID,
		CustomerID: in.CustomerID,
		FullName:   name,
		Phone:      phone,
		Source:     in.Source,
		Stage:      domain.StageNew,
		OwnerID:    in.OwnerID,
		Notes:      in.Notes,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, l); err != nil {
			return err
		}
		stage := domain.StageNew
		return s.repo.AppendStageHistory(ctx, &domain.StageHistory{
			ID:        uuid.New(),
			LeadID:    l.ID,
			FromStage: nil,
			ToStage:   stage,
			ChangedBy: in.ActorID,
			Note:      "created",
			CreatedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}

	s.bus.Publish(ctx, events.Event{Name: events.LeadCreated, Payload: l})
	return l, nil
}

func (s *Service) ChangeStage(ctx context.Context, leadID uuid.UUID, to domain.Stage, actorID uuid.UUID, note string) (*domain.Lead, error) {
	var out *domain.Lead
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		l, err := s.repo.FindByID(ctx, leadID)
		if err != nil {
			return shared.NewNotFound("lead")
		}
		from := l.Stage
		if err := l.TransitionTo(to); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, l); err != nil {
			return err
		}
		if err := s.repo.AppendStageHistory(ctx, &domain.StageHistory{
			ID:        uuid.New(),
			LeadID:    l.ID,
			FromStage: &from,
			ToStage:   to,
			ChangedBy: actorID,
			Note:      note,
			CreatedAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		out = l
		return nil
	})
	return out, err
}
