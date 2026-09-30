package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const maxPeriod = 366 * 24 * time.Hour

// KPI aggregates for Manager Dashboard (exceptions first) — T-084/T-085 / T-230.
type KPI struct {
	LeadsOpen      int       `json:"leads_open"`
	TasksOverdue   int       `json:"tasks_overdue"`
	BookingsUnpaid int       `json:"bookings_unpaid"`
	MissingDocs    int       `json:"missing_docs"`
	BookedAmt      int64     `json:"booked_amt"`
	CollectedAmt   int64     `json:"collected_amt"`
	MarginAmt      int64     `json:"margin_amt"`
	PeriodFrom     time.Time `json:"period_from"`
	PeriodTo       time.Time `json:"period_to"`
}

// Attention kinds, one per exception source. Each record appears under exactly one kind.
const (
	AttentionEscalatedTask = "escalated_task" // open task escalated by SLA rules
	AttentionOverdueTask   = "overdue_task"   // open non-document task past due
	AttentionMissingDoc    = "missing_doc"    // open document-collection task
	AttentionUnpaidBooking = "unpaid_booking" // balance with an overdue, imminent or missing instalment plan
	AttentionCapacity      = "capacity"       // upcoming departure closed, full or past its soft threshold
)

// AttentionKinds lists kinds in display order.
var AttentionKinds = []string{
	AttentionEscalatedTask, AttentionOverdueTask, AttentionUnpaidBooking, AttentionMissingDoc, AttentionCapacity,
}

// AttentionItem is an exception feed row — T-087.
type AttentionItem struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`     // one of AttentionKinds
	Severity    string    `json:"severity"` // medium|high
	Title       string    `json:"title"`
	RelatedType string    `json:"related_type"`
	RelatedID   uuid.UUID `json:"related_id"`
	AgeHours    int       `json:"age_hours"`
	HrefHint    string    `json:"href_hint"` // tasks|bookings|packages|pipeline
	// Context names who or what the row is about (customer, lead, package, target).
	Context string `json:"context,omitempty"`
	// LinkType/LinkID point at the record to open: booking|package|lead|conversation|revenue_target|task.
	LinkType string    `json:"link_type"`
	LinkID   uuid.UUID `json:"link_id"`
	// Amount is the open balance of an unpaid booking, in minor units of Currency.
	Amount        *int64     `json:"amount,omitempty"`
	Currency      string     `json:"currency,omitempty"`
	CapacitySold  *int       `json:"capacity_sold,omitempty"`
	CapacityTotal *int       `json:"capacity_total,omitempty"`
	DueAt         *time.Time `json:"due_at,omitempty"`
}

// AttentionSummary counts every open exception, not just the page the feed returns.
type AttentionSummary struct {
	Total int            `json:"total"`
	High  int            `json:"high"`
	Kinds map[string]int `json:"kinds"`
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

// AttentionReader reads the exception feed and its totals (ISP).
type AttentionReader interface {
	AttentionFeed(ctx context.Context, branchID *uuid.UUID, limit int) ([]AttentionItem, error)
	AttentionSummary(ctx context.Context, branchID *uuid.UUID) (*AttentionSummary, error)
}

// Aggregator is the persistence port (DIP).
type Aggregator interface {
	Compute(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*KPI, error)
	TeamPerformance(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]TeamMember, error)
	AttentionReader
	MyWorkToday(ctx context.Context, branchID *uuid.UUID, ownerID uuid.UUID, limit int) ([]MyWorkItem, error)
	TargetProgress(ctx context.Context, branchID uuid.UUID, ownerID *uuid.UUID) (*TargetProgress, error)
}

type Service struct {
	agg Aggregator
	now func() time.Time

	revenue      RevenueReader
	reportingCur ReportingCurrencyReader
	fx           fxdomain.Converter
}

func NewService(agg Aggregator) *Service {
	return &Service{agg: agg, now: func() time.Time { return time.Now().UTC() }}
}

// resolveBranch pins requested to the caller's scope: global callers may
// pick any branch (nil = all), everyone else gets their own branch. The
// aggregator further restricts owners for team/own scopes.
func resolveBranch(ctx context.Context, requested *uuid.UUID) (*uuid.UUID, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	return scope.ResolveBranch(requested)
}

func (s *Service) KPIs(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*KPI, error) {
	from, to, err := NormalizePeriod(from, to, s.now())
	if err != nil {
		return nil, err
	}
	if branchID, err = resolveBranch(ctx, branchID); err != nil {
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

func (s *Service) Attention(ctx context.Context, branchID *uuid.UUID, limit int) ([]AttentionItem, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	branchID, err := resolveBranch(ctx, branchID)
	if err != nil {
		return nil, err
	}
	return s.agg.AttentionFeed(ctx, branchID, limit)
}

// AttentionSummary totals open exceptions by kind; every kind is present, zero when clear.
func (s *Service) AttentionSummary(ctx context.Context, branchID *uuid.UUID) (*AttentionSummary, error) {
	branchID, err := resolveBranch(ctx, branchID)
	if err != nil {
		return nil, err
	}
	sum, err := s.agg.AttentionSummary(ctx, branchID)
	if err != nil {
		return nil, err
	}
	kinds := make(map[string]int, len(AttentionKinds))
	for _, k := range AttentionKinds {
		kinds[k] = sum.Kinds[k]
	}
	sum.Kinds = kinds
	return sum, nil
}

// MyWork is the caller's own queue across the branches their scope covers.
func (s *Service) MyWork(ctx context.Context, limit int) ([]MyWorkItem, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	if scope.UserID == uuid.Nil {
		return nil, shared.NewValidation("owner_id is required")
	}
	branchID, err := scope.ResolveBranch(nil)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	return s.agg.MyWorkToday(ctx, branchID, scope.UserID, limit)
}

// MyTarget reports target progress for branchID (the caller's branch when
// zero). A nil ownerID asks for the branch-level target, which only elevated
// scopes may see; own-level callers always get their personal target.
func (s *Service) MyTarget(ctx context.Context, branchID uuid.UUID, ownerID *uuid.UUID) (*TargetProgress, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	if branchID, err = scope.WriteBranch(branchID); err != nil {
		return nil, err
	}
	if !scope.IsElevated() && (ownerID == nil || *ownerID != scope.UserID) {
		me := scope.UserID
		ownerID = &me
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
