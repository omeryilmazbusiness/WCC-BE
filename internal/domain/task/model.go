package task

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Status string
type Kind string
type Priority string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusCancelled  Status = "cancelled"

	KindFollowUp Kind = "followup"
	KindDocument Kind = "document"
	KindPayment  Kind = "payment"
	KindCustom   Kind = "custom"

	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"

	// DefaultGrace is how long after due_at before a task is escalated (T-077).
	DefaultGrace = 24 * time.Hour
)

type Task struct {
	ID             uuid.UUID
	BranchID       uuid.UUID
	Title          string
	Kind           Kind
	Status         Status
	Priority       Priority
	Outcome        string
	AssigneeID     uuid.UUID
	AssigneeName   string // join enrichment (not persisted)
	RelatedType    string
	RelatedID      uuid.UUID
	DueAt          *time.Time
	EscalatedAt    *time.Time
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

type ListFilter struct {
	BranchID      *uuid.UUID
	AssigneeID    *uuid.UUID
	Status        Status
	Kind          Kind
	RelatedType   string
	RelatedID     *uuid.UUID
	OverdueOnly   bool
	EscalatedOnly bool
	Query         string
	Limit         int
	Offset        int
}

var taskTransitions = map[Status][]Status{
	StatusOpen:       {StatusInProgress, StatusDone, StatusCancelled},
	StatusInProgress: {StatusDone, StatusCancelled, StatusOpen},
	StatusDone:       {},
	StatusCancelled:  {},
}

func (t *Task) TransitionTo(to Status) error {
	for _, s := range taskTransitions[t.Status] {
		if s == to {
			t.Status = to
			t.UpdatedAt = time.Now().UTC()
			if to == StatusDone {
				now := time.Now().UTC()
				t.CompletedAt = &now
			}
			if to != StatusDone {
				t.CompletedAt = nil
			}
			return nil
		}
	}
	return shared.NewInvalidState("cannot transition task from " + string(t.Status) + " to " + string(to))
}

func (t *Task) Reschedule(due *time.Time) error {
	if t.Status == StatusDone || t.Status == StatusCancelled {
		return shared.NewInvalidState("cannot reschedule a closed task")
	}
	t.DueAt = due
	t.EscalatedAt = nil
	t.UpdatedAt = time.Now().UTC()
	return nil
}

func (t *Task) Assign(assigneeID uuid.UUID) error {
	if assigneeID == uuid.Nil {
		return shared.NewValidation("assignee_id is required")
	}
	if t.Status == StatusDone || t.Status == StatusCancelled {
		return shared.NewInvalidState("cannot reassign a closed task")
	}
	t.AssigneeID = assigneeID
	t.UpdatedAt = time.Now().UTC()
	return nil
}

func (t *Task) CompleteWithOutcome(outcome string) error {
	if err := t.TransitionTo(StatusDone); err != nil {
		return err
	}
	t.Outcome = outcome
	return nil
}

func (t *Task) IsOverdue(now time.Time) bool {
	if t.DueAt == nil {
		return false
	}
	if t.Status == StatusDone || t.Status == StatusCancelled {
		return false
	}
	return t.DueAt.Before(now)
}

// ShouldEscalate is true when due + grace has passed and not yet escalated.
func (t *Task) ShouldEscalate(now time.Time, grace time.Duration) bool {
	if t.EscalatedAt != nil || t.DueAt == nil {
		return false
	}
	if t.Status == StatusDone || t.Status == StatusCancelled {
		return false
	}
	if grace <= 0 {
		grace = DefaultGrace
	}
	return !t.DueAt.Add(grace).After(now)
}

func (t *Task) Escalate(now time.Time) {
	t.EscalatedAt = &now
	if t.Priority != PriorityUrgent {
		t.Priority = PriorityHigh
	}
	t.UpdatedAt = now
}

func ValidKind(k Kind) bool {
	switch k {
	case KindFollowUp, KindDocument, KindPayment, KindCustom:
		return true
	default:
		return false
	}
}

func ValidPriority(p Priority) bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent, "":
		return true
	default:
		return false
	}
}

type Repository interface {
	Create(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
	FindByID(ctx context.Context, id uuid.UUID) (*Task, error)
	FindByIdempotencyKey(ctx context.Context, key string) (*Task, error)
	List(ctx context.Context, f ListFilter) ([]Task, int, error)
	ListByAssignee(ctx context.Context, assigneeID uuid.UUID, status *Status, limit, offset int) ([]Task, int, error)
	ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]Task, error)
	CountOverdue(ctx context.Context, branchID *uuid.UUID) (int, error)
}
