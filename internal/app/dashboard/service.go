package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const maxPeriod = 366 * 24 * time.Hour // ~1 year (+ leap)

// KPI aggregates for Manager Dashboard (exceptions first).
//
// Period semantics (branch-scoped, half-open [from, to)):
//   - leads_open: open leads created in the period
//   - tasks_overdue: open/in_progress tasks whose due_at is in the period and already past now
//   - bookings_unpaid: confirmed bookings with balance > 0 created in the period
//   - missing_docs: open/in_progress document-kind tasks created in the period
type KPI struct {
	LeadsOpen      int       `json:"leads_open"`
	TasksOverdue   int       `json:"tasks_overdue"`
	BookingsUnpaid int       `json:"bookings_unpaid"`
	MissingDocs    int       `json:"missing_docs"`
	PeriodFrom     time.Time `json:"period_from"`
	PeriodTo       time.Time `json:"period_to"`
}

// Aggregator is the persistence port for KPI SQL (DIP).
type Aggregator interface {
	Compute(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*KPI, error)
}

type Service struct {
	agg Aggregator
	now func() time.Time
}

func NewService(agg Aggregator) *Service {
	return &Service{agg: agg, now: func() time.Time { return time.Now().UTC() }}
}

// KPIs validates the period window then delegates aggregation.
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

// NormalizePeriod enforces half-open [from, to) with sane bounds (SRP).
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
	// Reject far-future windows that would empty every card silently.
	if from.After(now.Add(24 * time.Hour)) {
		return time.Time{}, time.Time{}, shared.NewValidation("from must not be far in the future")
	}
	return from, to, nil
}

// DefaultPeriod returns the last 30 days ending at now (half-open).
func DefaultPeriod(now time.Time) (from, to time.Time) {
	to = now.UTC()
	from = to.AddDate(0, 0, -30)
	return from, to
}
