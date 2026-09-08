package task

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	leaddomain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Seeder listens to domain events and creates tasks idempotently.
type Seeder struct {
	tasks domain.Repository
	tx    *tx.Manager
}

func NewSeeder(tasks domain.Repository, txm *tx.Manager) *Seeder {
	return &Seeder{tasks: tasks, tx: txm}
}

func (s *Seeder) Register(bus *events.Bus) {
	bus.Subscribe(events.BookingConfirmed, s.onBookingConfirmed)
	bus.Subscribe(events.LeadCreated, s.onLeadCreated)
}

func (s *Seeder) onLeadCreated(ctx context.Context, ev events.Event) error {
	l, ok := ev.Payload.(*leaddomain.Lead)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	return s.ensureTask(ctx, domain.Task{
		ID:             uuid.New(),
		BranchID:       l.BranchID,
		Title:          "Follow up lead",
		Kind:           domain.KindFollowUp,
		Status:         domain.StatusOpen,
		AssigneeID:     l.OwnerID,
		RelatedType:    "lead",
		RelatedID:      l.ID,
		IdempotencyKey: fmt.Sprintf("lead:%s:followup", l.ID),
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

func (s *Seeder) onBookingConfirmed(ctx context.Context, ev events.Event) error {
	b, ok := ev.Payload.(*bookingdomain.Booking)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	seeds := []domain.Task{
		{
			ID:             uuid.New(),
			BranchID:       b.BranchID,
			Title:          "Collect documents",
			Kind:           domain.KindDocument,
			Status:         domain.StatusOpen,
			AssigneeID:     b.OwnerID,
			RelatedType:    "booking",
			RelatedID:      b.ID,
			IdempotencyKey: fmt.Sprintf("booking:%s:document", b.ID),
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		{
			ID:             uuid.New(),
			BranchID:       b.BranchID,
			Title:          "Collect payment",
			Kind:           domain.KindPayment,
			Status:         domain.StatusOpen,
			AssigneeID:     b.OwnerID,
			RelatedType:    "booking",
			RelatedID:      b.ID,
			IdempotencyKey: fmt.Sprintf("booking:%s:payment", b.ID),
			CreatedAt:      now,
			UpdatedAt:      now,
		},
	}
	for i := range seeds {
		if err := s.ensureTask(ctx, seeds[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Seeder) ensureTask(ctx context.Context, t domain.Task) error {
	if existing, err := s.tasks.FindByIdempotencyKey(ctx, t.IdempotencyKey); err == nil && existing != nil {
		return nil
	}
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing, err := s.tasks.FindByIdempotencyKey(ctx, t.IdempotencyKey); err == nil && existing != nil {
			return nil
		}
		return s.tasks.Create(ctx, &t)
	})
}

type Service struct {
	repo domain.Repository
}

func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) ListMine(ctx context.Context, assigneeID uuid.UUID, status *domain.Status, limit, offset int) ([]domain.Task, int, error) {
	return s.repo.ListByAssignee(ctx, assigneeID, status, limit, offset)
}
