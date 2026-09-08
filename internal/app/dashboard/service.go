package dashboard

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// KPI aggregates for Manager Dashboard (exceptions first).
type KPI struct {
	LeadsOpen       int `json:"leads_open"`
	TasksOverdue    int `json:"tasks_overdue"`
	BookingsUnpaid  int `json:"bookings_unpaid"`
	MissingDocs     int `json:"missing_docs"`
	PeriodFrom      time.Time `json:"period_from"`
	PeriodTo        time.Time `json:"period_to"`
}

// Aggregator port — implemented later with SQL aggregations (B10).
type Aggregator interface {
	Compute(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*KPI, error)
}

type Service struct {
	agg Aggregator
}

func NewService(agg Aggregator) *Service {
	return &Service{agg: agg}
}

func (s *Service) KPIs(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*KPI, error) {
	return s.agg.Compute(ctx, branchID, from, to)
}
