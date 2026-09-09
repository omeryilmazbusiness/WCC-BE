package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	leaddomain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID    uuid.UUID
	Title       string
	Kind        domain.Kind
	AssigneeID  uuid.UUID
	RelatedType string
	RelatedID   uuid.UUID
	DueAt       *time.Time
}

type Service struct {
	repo domain.Repository
	tx   *tx.Manager
	bus  *events.Bus
}

func NewService(repo domain.Repository, txm *tx.Manager, bus *events.Bus) *Service {
	return &Service{repo: repo, tx: txm, bus: bus}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Task, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, shared.NewValidation("title is required")
	}
	if !domain.ValidKind(in.Kind) {
		return nil, shared.NewValidation("invalid kind")
	}
	if in.AssigneeID == uuid.Nil || in.RelatedID == uuid.Nil {
		return nil, shared.NewValidation("assignee_id and related_id are required")
	}
	relatedType := strings.TrimSpace(in.RelatedType)
	if relatedType == "" {
		return nil, shared.NewValidation("related_type is required")
	}
	now := time.Now().UTC()
	t := &domain.Task{
		ID:          uuid.New(),
		BranchID:    in.BranchID,
		Title:       title,
		Kind:        in.Kind,
		Status:      domain.StatusOpen,
		AssigneeID:  in.AssigneeID,
		RelatedType: relatedType,
		RelatedID:   in.RelatedID,
		DueAt:       in.DueAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.Create(ctx, t)
	}); err != nil {
		return nil, err
	}
	s.bus.Publish(ctx, events.Event{Name: events.TaskCreated, Payload: t})
	return t, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	t, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("task")
	}
	return t, nil
}

func (s *Service) ListMine(ctx context.Context, assigneeID uuid.UUID, status *domain.Status, limit, offset int) ([]domain.Task, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return s.repo.ListByAssignee(ctx, assigneeID, status, limit, offset)
}

func (s *Service) ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]domain.Task, error) {
	return s.repo.ListByRelated(ctx, relatedType, relatedID)
}

func (s *Service) Transition(ctx context.Context, id uuid.UUID, to domain.Status) (*domain.Task, error) {
	var out *domain.Task
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("task")
		}
		if err := t.TransitionTo(to); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, t); err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

func (s *Service) Complete(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	return s.Transition(ctx, id, domain.StatusDone)
}

func (s *Service) Reschedule(ctx context.Context, id uuid.UUID, due *time.Time) (*domain.Task, error) {
	var out *domain.Task
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("task")
		}
		if err := t.Reschedule(due); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, t); err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}

// Seeder listens to domain events and creates tasks idempotently (B9 rules stub).
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
