package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type DashboardAggregator struct {
	pool *pgxpool.Pool
}

func NewDashboardAggregator(pool *pgxpool.Pool) *DashboardAggregator {
	return &DashboardAggregator{pool: pool}
}

func (a *DashboardAggregator) Compute(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*dashboard.KPI, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	kpi := &dashboard.KPI{PeriodFrom: from, PeriodTo: to}

	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM leads
		WHERE stage NOT IN ('won','lost')
		  AND created_at >= $1 AND created_at < $2
		  AND ($3::uuid IS NULL OR branch_id=$3)`, from, to, branchID).Scan(&kpi.LeadsOpen); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE status IN ('open','in_progress') AND due_at < NOW()
		  AND ($1::uuid IS NULL OR branch_id=$1)`, branchID).Scan(&kpi.TasksOverdue); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM bookings
		WHERE balance_amt > 0 AND status='confirmed'
		  AND ($1::uuid IS NULL OR branch_id=$1)`, branchID).Scan(&kpi.BookingsUnpaid); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE kind='document' AND status IN ('open','in_progress')
		  AND ($1::uuid IS NULL OR branch_id=$1)`, branchID).Scan(&kpi.MissingDocs); err != nil {
		return nil, err
	}
	return kpi, nil
}
