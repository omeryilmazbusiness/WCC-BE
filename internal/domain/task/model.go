package task

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Status string
type Kind string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusDone       Status = "done"
	StatusCancelled  Status = "cancelled"

	KindFollowUp Kind = "followup"
	KindDocument Kind = "document"
	KindPayment  Kind = "payment"
	KindCustom   Kind = "custom"
)

type Task struct {
	ID             uuid.UUID
	BranchID       uuid.UUID
	Title          string
	Kind           Kind
	Status         Status
	AssigneeID     uuid.UUID
	RelatedType    string
	RelatedID      uuid.UUID
	DueAt          *time.Time
	IdempotencyKey string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
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
	t.UpdatedAt = time.Now().UTC()
	return nil
}

func ValidKind(k Kind) bool {
	switch k {
	case KindFollowUp, KindDocument, KindPayment, KindCustom:
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
	ListByAssignee(ctx context.Context, assigneeID uuid.UUID, status *Status, limit, offset int) ([]Task, int, error)
	ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]Task, error)
	CountOverdue(ctx context.Context, branchID *uuid.UUID) (int, error)
}
