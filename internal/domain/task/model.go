package task

import (
	"context"
	"strings"
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

	// Importance of a task, most to least pressing.
	PriorityCritical Priority = "critical"
	PriorityMajor    Priority = "major"
	PriorityMinor    Priority = "minor"

	// DefaultGrace is how long after due_at before a task is escalated (T-077).
	DefaultGrace = 24 * time.Hour
)

// JobEscalateOverdue escalates overdue tasks across all branches on a schedule,
// so escalation never depends on someone opening the task screen.
const JobEscalateOverdue shared.JobName = "task.escalate_overdue"

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
	// SourceRule names the automation rule that created the task ("" = manual).
	SourceRule string
	// CreatedBy is the user who created a manual task (nil for automation).
	CreatedBy         *uuid.UUID
	OverdueNotifiedAt *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	CompletedAt       *time.Time
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
	if t.Priority != PriorityCritical {
		t.Priority = PriorityMajor
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
	case PriorityCritical, PriorityMajor, PriorityMinor:
		return true
	default:
		return false
	}
}

// ParsePriority reads an importance level. Empty means minor, and the retired
// low/normal/high/urgent values map onto the new scale so older clients keep working.
func ParsePriority(raw string) (Priority, error) {
	switch Priority(strings.ToLower(strings.TrimSpace(raw))) {
	case "", PriorityMinor, "low", "normal":
		return PriorityMinor, nil
	case PriorityMajor, "high":
		return PriorityMajor, nil
	case PriorityCritical, "urgent":
		return PriorityCritical, nil
	default:
		return "", shared.NewValidation("priority must be critical, major or minor")
	}
}

// ValidateRelated accepts a task linked to a record (type and id together) or a
// standalone one (neither); a half-filled link is rejected.
func ValidateRelated(relatedType string, relatedID uuid.UUID) error {
	if (strings.TrimSpace(relatedType) == "") != (relatedID == uuid.Nil) {
		return shared.NewValidation("related_type and related_id must be set together")
	}
	return nil
}

// MaxTitleLength bounds a task title (runes) so cards and notifications stay readable.
const MaxTitleLength = 200

// NormalizeTitle trims a title and rejects empty or overlong ones.
func NormalizeTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if title == "" {
		return "", shared.NewValidation("title is required")
	}
	if len([]rune(title)) > MaxTitleLength {
		return "", shared.NewValidation("title is too long")
	}
	return title, nil
}

// dueSkew tolerates clock drift between the client that picked a deadline and the server.
const dueSkew = 5 * time.Minute

// ValidateNewDue rejects a deadline already in the past when a task is created.
func ValidateNewDue(due *time.Time, now time.Time) error {
	if due != nil && due.Before(now.Add(-dueSkew)) {
		return shared.NewValidation("due_at must be in the future")
	}
	return nil
}

// HasRelated reports whether the task points at a record; manual tasks may stand alone.
func (t *Task) HasRelated() bool { return t.RelatedID != uuid.Nil }

type Repository interface {
	Create(ctx context.Context, t *Task) error
	Update(ctx context.Context, t *Task) error
	FindByID(ctx context.Context, id uuid.UUID) (*Task, error)
	FindByIdempotencyKey(ctx context.Context, key string) (*Task, error)
	List(ctx context.Context, f ListFilter) ([]Task, int, error)
	ListByAssignee(ctx context.Context, assigneeID uuid.UUID, status *Status, limit, offset int) ([]Task, int, error)
	ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]Task, error)
	CountOverdue(ctx context.Context, branchID *uuid.UUID) (int, error)
	// ListOpenByRule returns open tasks a rule created for one record.
	ListOpenByRule(ctx context.Context, rule, relatedType string, relatedID uuid.UUID) ([]Task, error)
	// ListOverdueUnnotified returns open overdue tasks not yet announced.
	ListOverdueUnnotified(ctx context.Context, now time.Time, limit int) ([]Task, error)
	// MarkOverdueNotified stamps the announcement once; false when already stamped.
	MarkOverdueNotified(ctx context.Context, id uuid.UUID, at time.Time) (bool, error)
}
