package task

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	RelatedType    string // lead | booking | customer
	RelatedID      uuid.UUID
	DueAt          *time.Time
	IdempotencyKey string // for auto-seeded tasks on booking.confirmed
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

type Repository interface {
	Create(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
	FindByID(ctx context.Context, id uuid.UUID) (*Task, error)
	FindByIdempotencyKey(ctx context.Context, key string) (*Task, error)
	ListByAssignee(ctx context.Context, assigneeID uuid.UUID, status *Status, limit, offset int) ([]Task, int, error)
	CountOverdue(ctx context.Context, branchID *uuid.UUID) (int, error)
}
