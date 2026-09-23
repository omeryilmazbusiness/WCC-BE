package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const maxPeriod = 366 * 24 * time.Hour

// KPI aggregates for Manager Dashboard (exceptions first) — T-084/T-085.
type KPI struct {
	LeadsOpen      int       `json:"leads_open"`
	TasksOverdue   int       `json:"tasks_overdue"`
	BookingsUnpaid int       `json:"bookings_unpaid"`
	MissingDocs    int       `json:"missing_docs"`
	PeriodFrom     time.Time `json:"period_from"`
	PeriodTo       time.Time `json:"period_to"`
}

// TeamMember is per-owner performance for the manager board — T-086.
type TeamMember struct {
	OwnerID      uuid.UUID `json:"owner_id"`
	OwnerName    string    `json:"owner_name"`
	LeadsHandled int       `json:"leads_handled"`
	LeadsWon     int       `json:"leads_won"`
	OpenTasks    int       `json:"open_tasks"`
	OverdueTasks int       `json:"overdue_tasks"`
	CollectedAmt int64     `json:"collected_amt"`
}

// AttentionItem is an exception feed row — T-087.
type AttentionItem struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"` // overdue_task|unpaid_booking|missing_doc|capacity|escalated_task
	Severity    string    `json:"severity"` // low|medium|high
	Title       string    `json:"title"`
	RelatedType string    `json:"related_type"`
	RelatedID   uuid.UUID `json:"related_id"`
	AgeHours    int       `json:"age_hours"`
	HrefHint    string    `json:"href_hint"` // tasks|bookings|packages|pipeline
}

// MyWorkItem is a prioritized employee work queue row — T-089.
type MyWorkItem struct {
	ID          uuid.UUID  `json:"id"`
	Source      string     `json:"source"` // task|lead
	Title       string     `json:"title"`
	Kind        string     `json:"kind"`
	Priority    int        `json:"priority"` // lower = sooner
	DueAt       *time.Time `json:"due_at,omitempty"`
	RelatedType string     `json:"related_type"`
	RelatedID   uuid.UUID  `json:"related_id"`
	Overdue     bool       `json:"overdue"`
	Escalated   bool       `json:"escalated"`
}

// TargetProgress is personal/branch target snapshot — T-090.
type TargetProgress struct {
	Label          string `json:"label"`
	TargetAmount   int64  `json:"target_amount"`
	ActualAmount   int64  `json:"actual_amount"`
	ExpectedToDate int64  `json:"expected_to_date"`
	Currency       string `json:"currency"`
	Status         string `json:"status"` // ahead|on_track|behind|placeholder
	PeriodStart    string `json:"period_start"`
	PeriodEnd      string `json:"period_end"`
}

// Scope carries consistent filter dimensions — T-088.
type Scope struct {
	BranchID *uuid.UUID
	OwnerID  *uuid.UUID
	From     time.Time
	To       time.Time
}

// Aggregator is the persistence port (DIP).
type Aggregator interface {
	Compute(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*KPI, error)
	TeamPerformance(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]TeamMember, error)
	AttentionFeed(ctx context.Context, branchID *uuid.UUID, limit int) ([]AttentionItem, error)
	MyWorkToday(ctx context.Context, branchID, ownerID uuid.UUID, limit int) ([]MyWorkItem, error)
	TargetProgress(ctx context.Context, branchID uuid.UUID, ownerID *uuid.UUID) (*TargetProgress, error)
}

type Service struct {
	agg Aggregator
	now func() time.Time
}

func NewService(agg Aggregator) *Service {
	return &Service{agg: agg, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) KPIs(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*KPI, error) {
	from, to, err := NormalizePeriod(from, to, s.now())
	if err != nil {
		return nil, err
	}
	kpi, err := s.agg.Compute(ctx, branchID, from, to)
	if err != nil {
		return nil, err
	}
	kpi.PeriodFrom = from
	kpi.PeriodTo = to
	return kpi, nil
}

func (s *Service) Team(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]TeamMember, error) {
	from, to, err := NormalizePeriod(from, to, s.now())
	if err != nil {
		return nil, err
	}
	return s.agg.TeamPerformance(ctx, branchID, from, to)
}

func (s *Service) Attention(ctx context.Context, branchID *uuid.UUID, limit int) ([]AttentionItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.agg.AttentionFeed(ctx, branchID, limit)
}

func (s *Service) MyWork(ctx context.Context, branchID, ownerID uuid.UUID, limit int) ([]MyWorkItem, error) {
	if ownerID == uuid.Nil {
		return nil, shared.NewValidation("owner_id is required")
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.agg.MyWorkToday(ctx, branchID, ownerID, limit)
}

func (s *Service) MyTarget(ctx context.Context, branchID uuid.UUID, ownerID *uuid.UUID) (*TargetProgress, error) {
	if branchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	return s.agg.TargetProgress(ctx, branchID, ownerID)
}

func NormalizePeriod(from, to, now time.Time) (time.Time, time.Time, error) {
	if from.IsZero() || to.IsZero() {
		return time.Time{}, time.Time{}, shared.NewValidation("from and to are required")
	}
	from = from.UTC()
	to = to.UTC()
	if !from.Before(to) {
		return time.Time{}, time.Time{}, shared.NewValidation("from must be before to")
	}
	if to.Sub(from) > maxPeriod {
		return time.Time{}, time.Time{}, shared.NewValidation("period must be at most 1 year")
	}
	if from.After(now.Add(24 * time.Hour)) {
		return time.Time{}, time.Time{}, shared.NewValidation("from must not be far in the future")
	}
	return from, to, nil
}

func DefaultPeriod(now time.Time) (from, to time.Time) {
	to = now.UTC()
	from = to.AddDate(0, 0, -30)
	return from, to
}

func TargetStatus(actual, expected int64) string {
	if expected <= 0 {
		if actual > 0 {
			return "ahead"
		}
		return "on_track"
	}
	ratio := float64(actual) / float64(expected)
	switch {
	case ratio >= 1.05:
		return "ahead"
	case ratio >= 0.9:
		return "on_track"
	default:
		return "behind"
	}
}
