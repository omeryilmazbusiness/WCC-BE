package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
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
	Description string
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

type CancelInput struct {
	Reason string
}

type ListInput struct {
	BranchID      *uuid.UUID
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
	repo   domain.Repository
	tx     tx.Runner
	bus    *events.Bus
	conv   ConversationReader
	outbox events.Outbox
	grace  GracePolicy
	now    func() time.Time
}

// GracePolicy decides how long an overdue task of a rule may stay open in a
// branch before escalating; enabled=false turns escalation off (DIP).
type GracePolicy interface {
	Grace(ctx context.Context, branchID uuid.UUID, rule string) (grace time.Duration, enabled bool)
}

func (s *Service) SetOutbox(o events.Outbox)    { s.outbox = o }
func (s *Service) SetGracePolicy(g GracePolicy) { s.grace = g }

// ConversationReader loads inbox conversations for next-task flows (ISP / DIP).
type ConversationReader interface {
	GetConversation(ctx context.Context, id uuid.UUID) (*ConversationRef, error)
}

type ConversationRef struct {
	ID         uuid.UUID
	BranchID   uuid.UUID
	OwnerID    *uuid.UUID
	LeadID     *uuid.UUID
	CustomerID *uuid.UUID
}

func NewService(repo domain.Repository, txm tx.Runner, bus *events.Bus) *Service {
	return &Service{repo: repo, tx: txm, bus: bus, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetConversationReader(r ConversationReader) { s.conv = r }

// Create opens a task by hand. It may link a record (lead, booking, ...) or stand
// alone; importance defaults to minor and a deadline must not lie in the past.
func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Task, error) {
	title, err := domain.NormalizeTitle(in.Title)
	if err != nil {
		return nil, err
	}
	description, err := domain.NormalizeDescription(in.Description)
	if err != nil {
		return nil, err
	}
	if !domain.ValidKind(in.Kind) {
		return nil, shared.NewValidation("invalid kind")
	}
	prio, err := domain.ParsePriority(string(in.Priority))
	if err != nil {
		return nil, err
	}
	relatedType := strings.TrimSpace(in.RelatedType)
	if err := domain.ValidateRelated(relatedType, in.RelatedID); err != nil {
		return nil, err
	}
	if err := domain.ValidateNewDue(in.DueAt, s.now()); err != nil {
		return nil, err
	}
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	branchID, err := scope.WriteBranch(in.BranchID)
	if err != nil {
		return nil, err
	}
	assigneeID, err := scope.ResolveOwner(in.AssigneeID)
	if err != nil {
		return nil, err
	}
	if assigneeID == uuid.Nil {
		return nil, shared.NewValidation("assignee_id is required")
	}
	now := s.now()
	t := &domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: title, Description: description, Kind: in.Kind, Priority: prio,
		Status: domain.StatusOpen, AssigneeID: assigneeID, RelatedType: relatedType,
		RelatedID: in.RelatedID, DueAt: in.DueAt, CreatedAt: now, UpdatedAt: now,
	}
	if scope.UserID != uuid.Nil {
		creator := scope.UserID
		t.CreatedBy = &creator
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

// List pins the branch filter to the caller's scope; the repository also
// restricts assignees, so in.AssigneeID can only narrow visibility.
func (s *Service) List(ctx context.Context, in ListInput) ([]domain.Task, int, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, 0, err
	}
	branchID, err := scope.ResolveBranch(in.BranchID)
	if err != nil {
		return nil, 0, err
	}
	f := domain.ListFilter{
		BranchID: branchID, AssigneeID: in.AssigneeID, Status: in.Status, Kind: in.Kind,
		RelatedType: in.RelatedType, RelatedID: in.RelatedID,
		OverdueOnly: in.OverdueOnly, EscalatedOnly: in.EscalatedOnly,
		Query: in.Query, Limit: in.Limit, Offset: in.Offset,
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

func (s *Service) Cancel(ctx context.Context, id uuid.UUID, in CancelInput) (*domain.Task, error) {
	var out *domain.Task
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("task")
		}
		if err := t.CancelWithReason(strings.TrimSpace(in.Reason)); err != nil {
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
	if err := s.checkAssignee(ctx, in.AssigneeID); err != nil {
		return nil, err
	}
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
	if err := s.checkAssignee(ctx, in.AssigneeID); err != nil {
		return nil, err
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

// checkAssignee rejects handing a task to someone else unless the caller's
// scope allows reassignment.
func (s *Service) checkAssignee(ctx context.Context, assigneeID uuid.UUID) error {
	if assigneeID == uuid.Nil {
		return nil
	}
	scope, err := access.Require(ctx)
	if err != nil {
		return err
	}
	_, err = scope.ResolveOwner(assigneeID)
	return err
}

// EscalateOverdue marks open tasks past due+grace as escalated (T-077) among
// those visible to the caller; requested nil means all branches for global
// callers and the caller's branch otherwise.
func (s *Service) EscalateOverdue(ctx context.Context, requested *uuid.UUID) (int, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return 0, err
	}
	branchID, err := scope.ResolveBranch(requested)
	if err != nil {
		return 0, err
	}
	items, _, err := s.repo.List(ctx, domain.ListFilter{
		BranchID: branchID, OverdueOnly: true, Limit: 500,
	})
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	var escalated []domain.Task
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		for i := range items {
			t := items[i]
			grace, enabled := s.graceFor(ctx, t)
			if !enabled || !t.ShouldEscalate(now, grace) {
				continue
			}
			t.Escalate(now)
			if err := s.repo.Update(ctx, &t); err != nil {
				return err
			}
			escalated = append(escalated, t)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for i := range escalated {
		cp := escalated[i]
		s.bus.Publish(ctx, events.Event{Name: events.TaskEscalated, Payload: &cp})
	}
	return len(escalated), nil
}

func (s *Service) graceFor(ctx context.Context, t domain.Task) (time.Duration, bool) {
	if s.grace == nil {
		return domain.Grace(t.SourceRule, nil), true
	}
	return s.grace.Grace(ctx, t.BranchID, t.SourceRule)
}

// AnnounceOverdue records task.overdue once per overdue episode (a due_at change
// starts a new one); the stamp and the event commit together.
func (s *Service) AnnounceOverdue(ctx context.Context, limit int) (int, error) {
	now := s.now()
	items, err := s.repo.ListOverdueUnnotified(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range items {
		t := items[i]
		err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
			stamped, err := s.repo.MarkOverdueNotified(ctx, t.ID, now)
			if err != nil || !stamped {
				return err
			}
			n++
			return events.Record(ctx, s.outbox, events.Event{Name: events.TaskOverdue, Payload: events.TaskOverduePayload{
				TaskID: t.ID, BranchID: t.BranchID, AssigneeID: t.AssigneeID, Title: t.Title, DueAt: *t.DueAt,
			}})
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// SuggestionDTO is the HTTP-facing next-task proposal (never auto-created).
type SuggestionDTO struct {
	Title    string    `json:"title"`
	Kind     string    `json:"kind"`
	DueAt    time.Time `json:"due_at"`
	Priority int       `json:"priority"`
	Reason   string    `json:"reason"`
	Outcome  string    `json:"outcome"`
}

func (s *Service) SuggestNextTask(ctx context.Context, conversationID uuid.UUID, outcome string) (*SuggestionDTO, error) {
	if s.conv == nil {
		return nil, shared.NewValidation("conversation reader not configured")
	}
	c, err := s.conv.GetConversation(ctx, conversationID)
	if err != nil || c == nil {
		return nil, shared.NewNotFound("conversation")
	}
	sug, ok := domain.SuggestNextTask(domain.CallOutcome(outcome), s.now())
	if !ok {
		return nil, shared.NewValidation("outcome does not require a next task")
	}
	return &SuggestionDTO{
		Title: sug.Title, Kind: sug.Kind, DueAt: domain.SuggestedDueAt(sug, s.now()),
		Priority: sug.Priority, Reason: sug.Reason, Outcome: strings.ToLower(strings.TrimSpace(outcome)),
	}, nil
}

type ConfirmNextTaskInput struct {
	ConversationID uuid.UUID
	ActorID        uuid.UUID
	Outcome        string
	Title          string
	Kind           string
}

func (s *Service) ConfirmNextTask(ctx context.Context, in ConfirmNextTaskInput) (*domain.Task, error) {
	if s.conv == nil {
		return nil, shared.NewValidation("conversation reader not configured")
	}
	c, err := s.conv.GetConversation(ctx, in.ConversationID)
	if err != nil || c == nil {
		return nil, shared.NewNotFound("conversation")
	}
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	if !scope.CanAccessBranch(c.BranchID) {
		return nil, shared.NewNotFound("conversation")
	}
	outcome := strings.ToLower(strings.TrimSpace(in.Outcome))
	sug, ok := domain.SuggestNextTask(domain.CallOutcome(outcome), s.now())
	if !ok {
		return nil, shared.NewValidation("outcome does not require a next task")
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = sug.Title
	}
	kindStr := strings.TrimSpace(in.Kind)
	if kindStr == "" {
		kindStr = sug.Kind
	}
	kind := mapSuggestionKind(kindStr)
	prio := mapSuggestionPriority(sug.Priority)
	assignee := in.ActorID
	if c.OwnerID != nil && *c.OwnerID != uuid.Nil {
		assignee = *c.OwnerID
	}
	relatedType := "conversation"
	relatedID := c.ID
	if c.LeadID != nil && *c.LeadID != uuid.Nil {
		relatedType = "lead"
		relatedID = *c.LeadID
	} else if c.CustomerID != nil && *c.CustomerID != uuid.Nil {
		relatedType = "customer"
		relatedID = *c.CustomerID
	}
	due := domain.SuggestedDueAt(sug, s.now())
	key := fmt.Sprintf("conv:%s:outcome:%s", c.ID, outcome)
	if existing, err := s.repo.FindByIdempotencyKey(ctx, key); err == nil && existing != nil {
		return existing, nil
	}
	now := s.now()
	t := &domain.Task{
		ID: uuid.New(), BranchID: c.BranchID, Title: title, Kind: kind, Priority: prio,
		Status: domain.StatusOpen, AssigneeID: assignee, RelatedType: relatedType,
		RelatedID: relatedID, DueAt: &due, IdempotencyKey: key, SourceRule: domain.RuleConversationOutcome,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if existing, err := s.repo.FindByIdempotencyKey(txCtx, key); err == nil && existing != nil {
			t = existing
			return nil
		}
		return s.repo.Create(txCtx, t)
	}); err != nil {
		return nil, err
	}
	s.bus.Publish(ctx, events.Event{Name: events.TaskCreated, Payload: t})
	return t, nil
}

func mapSuggestionKind(k string) domain.Kind {
	switch strings.ToLower(strings.TrimSpace(k)) {
	case "follow_up", "followup":
		return domain.KindFollowUp
	case "document", "docs_pending":
		return domain.KindDocument
	case "payment", "payment_due":
		return domain.KindPayment
	default:
		return domain.KindCustom
	}
}

func mapSuggestionPriority(p int) domain.Priority {
	switch {
	case p <= 1:
		return domain.PriorityMajor
	case p == 2:
		return domain.PriorityMinor
	default:
		return domain.PriorityMinor
	}
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
		Kind: domain.KindFollowUp, Priority: domain.PriorityMinor, Status: domain.StatusOpen,
		AssigneeID: l.OwnerID, RelatedType: "lead", RelatedID: l.ID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("lead:%s:followup", l.ID), SourceRule: domain.RuleLeadFollowUp,
		CreatedAt: now, UpdatedAt: now,
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
			Kind: domain.KindDocument, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
			AssigneeID: b.OwnerID, RelatedType: "booking", RelatedID: b.ID, DueAt: &dueDoc,
			IdempotencyKey: fmt.Sprintf("booking:%s:document", b.ID), SourceRule: domain.RuleBookingDocuments,
			CreatedAt: now, UpdatedAt: now,
		},
		{
			ID: uuid.New(), BranchID: b.BranchID, Title: "Collect payment",
			Kind: domain.KindPayment, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
			AssigneeID: b.OwnerID, RelatedType: "booking", RelatedID: b.ID, DueAt: &duePay,
			IdempotencyKey: fmt.Sprintf("booking:%s:payment", b.ID), SourceRule: domain.RuleBookingPayment,
			CreatedAt: now, UpdatedAt: now,
		},
	}
	for i := range seeds {
		if err := s.ensureTask(ctx, seeds[i]); err != nil {
			return err
		}
	}
	return nil
}

// ensureTask creates a system-generated task once per idempotency key. It
// runs with system scope even when invoked inside a user request (payment
// reminders, target recovery): the task is assigned to the record owner, not
// the caller, and idempotency keys are global, so both the dedupe lookup and
// the insert must see every task. Callers must only pass records the user was
// already authorized to act on.
func (s *Seeder) ensureTask(ctx context.Context, t domain.Task) error {
	ctx = access.WithScope(ctx, access.System())
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
		Kind: domain.KindPayment, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
		AssigneeID: actorID, RelatedType: "booking", RelatedID: bookingID, DueAt: &dueAt,
		IdempotencyKey: fmt.Sprintf("booking:%s:payment-due:%s", bookingID, dueAt.UTC().Format("2006-01-02")),
		SourceRule:     domain.RulePaymentDue,
		CreatedAt:      now, UpdatedAt: now,
	})
}

// EnsureHoldExpiredTask asks the booking owner to follow up on a lapsed
// option hold (T-269); one task per hold expiry.
func (s *Seeder) EnsureHoldExpiredTask(ctx context.Context, branchID, bookingID, ownerID uuid.UUID, expiredAt time.Time) error {
	now := time.Now().UTC()
	due := now.Add(24 * time.Hour)
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: "Option hold expired: follow up with customer",
		Kind: domain.KindFollowUp, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
		AssigneeID: ownerID, RelatedType: "booking", RelatedID: bookingID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("booking:%s:hold-expired:%s", bookingID, expiredAt.UTC().Format(time.RFC3339)),
		SourceRule:     domain.RuleHoldExpired,
		CreatedAt:      now, UpdatedAt: now,
	})
}

// EnsureTargetRecoveryTask creates a once-per-day recovery task when a target is behind pace.
func (s *Seeder) EnsureTargetRecoveryTask(ctx context.Context, branchID, targetID, assigneeID uuid.UUID, label string, deficit int64) error {
	now := time.Now().UTC()
	asOf := now.Format("2006-01-02")
	due := now.Add(24 * time.Hour)
	title := fmt.Sprintf("Recover target: %s (deficit %d)", label, deficit)
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: title,
		Kind: domain.KindCustom, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
		AssigneeID: assigneeID, RelatedType: "revenue_target", RelatedID: targetID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("target:%s:recovery:%s", targetID, asOf),
		SourceRule:     domain.RuleTargetRecovery,
		CreatedAt:      now, UpdatedAt: now,
	})
}
