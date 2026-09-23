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
	Priority    domain.Priority
	AssigneeID  uuid.UUID
	RelatedType string
	RelatedID   uuid.UUID
	DueAt       *time.Time
}

type AssignInput struct {
	AssigneeID uuid.UUID
}

type BulkAssignInput struct {
	TaskIDs    []uuid.UUID
	AssigneeID uuid.UUID
}

type CompleteInput struct {
	Outcome string
}

type ListInput struct {
	BranchID      uuid.UUID
	AssigneeID    *uuid.UUID
	Status        domain.Status
	Kind          domain.Kind
	RelatedType   string
	RelatedID     *uuid.UUID
	OverdueOnly   bool
	EscalatedOnly bool
	Query         string
	Limit         int
	Offset        int
}

type Service struct {
	repo domain.Repository
	tx   tx.Runner
	bus  *events.Bus
}

func NewService(repo domain.Repository, txm tx.Runner, bus *events.Bus) *Service {
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
	if !domain.ValidPriority(in.Priority) {
		return nil, shared.NewValidation("invalid priority")
	}
	if in.AssigneeID == uuid.Nil || in.RelatedID == uuid.Nil {
		return nil, shared.NewValidation("assignee_id and related_id are required")
	}
	relatedType := strings.TrimSpace(in.RelatedType)
	if relatedType == "" {
		return nil, shared.NewValidation("related_type is required")
	}
	prio := in.Priority
	if prio == "" {
		prio = domain.PriorityNormal
	}
	now := time.Now().UTC()
	t := &domain.Task{
		ID: uuid.New(), BranchID: in.BranchID, Title: title, Kind: in.Kind, Priority: prio,
		Status: domain.StatusOpen, AssigneeID: in.AssigneeID, RelatedType: relatedType,
		RelatedID: in.RelatedID, DueAt: in.DueAt, CreatedAt: now, UpdatedAt: now,
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

func (s *Service) List(ctx context.Context, in ListInput) ([]domain.Task, int, error) {
	f := domain.ListFilter{
		AssigneeID: in.AssigneeID, Status: in.Status, Kind: in.Kind,
		RelatedType: in.RelatedType, RelatedID: in.RelatedID,
		OverdueOnly: in.OverdueOnly, EscalatedOnly: in.EscalatedOnly,
		Query: in.Query, Limit: in.Limit, Offset: in.Offset,
	}
	if in.BranchID != uuid.Nil {
		bid := in.BranchID
		f.BranchID = &bid
	}
	return s.repo.List(ctx, f)
}

func (s *Service) ListMine(ctx context.Context, assigneeID uuid.UUID, status *domain.Status, limit, offset int) ([]domain.Task, int, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
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

func (s *Service) Complete(ctx context.Context, id uuid.UUID, in CompleteInput) (*domain.Task, error) {
	var out *domain.Task
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("task")
		}
		if err := t.CompleteWithOutcome(strings.TrimSpace(in.Outcome)); err != nil {
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

func (s *Service) Assign(ctx context.Context, id uuid.UUID, in AssignInput) (*domain.Task, error) {
	var out *domain.Task
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("task")
		}
		if err := t.Assign(in.AssigneeID); err != nil {
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

func (s *Service) BulkAssign(ctx context.Context, in BulkAssignInput) ([]domain.Task, error) {
	if in.AssigneeID == uuid.Nil {
		return nil, shared.NewValidation("assignee_id is required")
	}
	if len(in.TaskIDs) == 0 {
		return nil, shared.NewValidation("task_ids are required")
	}
	var out []domain.Task
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		for _, id := range in.TaskIDs {
			t, err := s.repo.FindByID(ctx, id)
			if err != nil {
				return shared.NewNotFound("task")
			}
			if err := t.Assign(in.AssigneeID); err != nil {
				return err
			}
			if err := s.repo.Update(ctx, t); err != nil {
				return err
			}
			out = append(out, *t)
		}
		return nil
	})
	return out, err
}

// EscalateOverdue marks open tasks past due+grace as escalated (T-077).
func (s *Service) EscalateOverdue(ctx context.Context, branchID uuid.UUID) (int, error) {
	items, _, err := s.repo.List(ctx, domain.ListFilter{
		BranchID: &branchID, OverdueOnly: true, Limit: 500,
	})
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	n := 0
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		for i := range items {
			t := items[i]
			if !t.ShouldEscalate(now, domain.DefaultGrace) {
				continue
			}
			t.Escalate(now)
			if err := s.repo.Update(ctx, &t); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// Seeder listens to domain events and creates tasks idempotently (T-075/T-076).
type Seeder struct {
	tasks domain.Repository
	tx    tx.Runner
}

func NewSeeder(tasks domain.Repository, txm tx.Runner) *Seeder {
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
	due := now.Add(24 * time.Hour)
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: l.BranchID, Title: "Follow up lead",
		Kind: domain.KindFollowUp, Priority: domain.PriorityNormal, Status: domain.StatusOpen,
		AssigneeID: l.OwnerID, RelatedType: "lead", RelatedID: l.ID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("lead:%s:followup", l.ID), CreatedAt: now, UpdatedAt: now,
	})
}

func (s *Seeder) onBookingConfirmed(ctx context.Context, ev events.Event) error {
	b, ok := ev.Payload.(*bookingdomain.Booking)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	dueDoc := now.Add(72 * time.Hour)
	duePay := now.Add(48 * time.Hour)
	seeds := []domain.Task{
		{
			ID: uuid.New(), BranchID: b.BranchID, Title: "Collect documents",
			Kind: domain.KindDocument, Priority: domain.PriorityHigh, Status: domain.StatusOpen,
			AssigneeID: b.OwnerID, RelatedType: "booking", RelatedID: b.ID, DueAt: &dueDoc,
			IdempotencyKey: fmt.Sprintf("booking:%s:document", b.ID), CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: uuid.New(), BranchID: b.BranchID, Title: "Collect payment",
			Kind: domain.KindPayment, Priority: domain.PriorityHigh, Status: domain.StatusOpen,
			AssigneeID: b.OwnerID, RelatedType: "booking", RelatedID: b.ID, DueAt: &duePay,
			IdempotencyKey: fmt.Sprintf("booking:%s:payment", b.ID), CreatedAt: now, UpdatedAt: now,
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

// EnsurePaymentDueTask creates a payment-due reminder task (T-123).
func (s *Seeder) EnsurePaymentDueTask(ctx context.Context, branchID, bookingID, actorID uuid.UUID, dueAt time.Time, amount int64, currency string) error {
	now := time.Now().UTC()
	title := fmt.Sprintf("Payment due %d %s", amount, currency)
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: title,
		Kind: domain.KindPayment, Priority: domain.PriorityHigh, Status: domain.StatusOpen,
		AssigneeID: actorID, RelatedType: "booking", RelatedID: bookingID, DueAt: &dueAt,
		IdempotencyKey: fmt.Sprintf("booking:%s:payment-due:%s", bookingID, dueAt.UTC().Format("2006-01-02")),
		CreatedAt: now, UpdatedAt: now,
	})
}
