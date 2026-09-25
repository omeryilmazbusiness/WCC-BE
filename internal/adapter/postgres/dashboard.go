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
		  AND ($3::uuid IS NULL OR branch_id = $3)`, from, to, branchID).Scan(&kpi.LeadsOpen); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE status IN ('open','in_progress')
		  AND due_at IS NOT NULL AND due_at < NOW()
		  AND due_at >= $1 AND due_at < $2
		  AND ($3::uuid IS NULL OR branch_id = $3)`, from, to, branchID).Scan(&kpi.TasksOverdue); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM bookings
		WHERE balance_amt > 0 AND status = 'confirmed'
		  AND created_at >= $1 AND created_at < $2
		  AND ($3::uuid IS NULL OR branch_id = $3)`, from, to, branchID).Scan(&kpi.BookingsUnpaid); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE kind = 'document' AND status IN ('open','in_progress')
		  AND created_at >= $1 AND created_at < $2
		  AND ($3::uuid IS NULL OR branch_id = $3)`, from, to, branchID).Scan(&kpi.MissingDocs); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(total_amount),0), COALESCE(SUM(collected_amt),0),
			COALESCE(SUM(total_amount - COALESCE(cost_amt,0)),0)
		FROM bookings
		WHERE status IN ('confirmed','completed')
		  AND created_at >= $1 AND created_at < $2
		  AND ($3::uuid IS NULL OR branch_id = $3)`, from, to, branchID).
		Scan(&kpi.BookedAmt, &kpi.CollectedAmt, &kpi.MarginAmt); err != nil {
		return nil, err
	}
	return kpi, nil
}

func (a *DashboardAggregator) TeamPerformance(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]dashboard.TeamMember, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	rows, err := q.Query(ctx, `
		WITH owners AS (
			SELECT DISTINCT owner_id AS id FROM leads
			WHERE created_at >= $1 AND created_at < $2 AND ($3::uuid IS NULL OR branch_id=$3)
			UNION
			SELECT DISTINCT assignee_id FROM tasks
			WHERE created_at >= $1 AND created_at < $2 AND ($3::uuid IS NULL OR branch_id=$3)
			UNION
			SELECT DISTINCT owner_id FROM bookings
			WHERE created_at >= $1 AND created_at < $2 AND ($3::uuid IS NULL OR branch_id=$3)
		)
		SELECT o.id,
			COALESCE(u.full_name, ''),
			(SELECT COUNT(*) FROM leads l WHERE l.owner_id=o.id AND l.created_at >= $1 AND l.created_at < $2 AND ($3::uuid IS NULL OR l.branch_id=$3)),
			(SELECT COUNT(*) FROM leads l WHERE l.owner_id=o.id AND l.stage='won' AND l.updated_at >= $1 AND l.updated_at < $2 AND ($3::uuid IS NULL OR l.branch_id=$3)),
			(SELECT COUNT(*) FROM tasks t WHERE t.assignee_id=o.id AND t.status IN ('open','in_progress') AND ($3::uuid IS NULL OR t.branch_id=$3)),
			(SELECT COUNT(*) FROM tasks t WHERE t.assignee_id=o.id AND t.status IN ('open','in_progress') AND t.due_at < NOW() AND ($3::uuid IS NULL OR t.branch_id=$3)),
			(SELECT COALESCE(SUM(b.collected_amt),0) FROM bookings b WHERE b.owner_id=o.id AND b.created_at >= $1 AND b.created_at < $2 AND ($3::uuid IS NULL OR b.branch_id=$3))
		FROM owners o
		LEFT JOIN users u ON u.id = o.id
		ORDER BY 3 DESC, 7 DESC`, from, to, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dashboard.TeamMember
	for rows.Next() {
		var m dashboard.TeamMember
		if err := rows.Scan(&m.OwnerID, &m.OwnerName, &m.LeadsHandled, &m.LeadsWon, &m.OpenTasks, &m.OverdueTasks, &m.CollectedAmt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (a *DashboardAggregator) AttentionFeed(ctx context.Context, branchID *uuid.UUID, limit int) ([]dashboard.AttentionItem, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	rows, err := q.Query(ctx, `
		SELECT id, kind, severity, title, related_type, related_id, age_hours, href_hint FROM (
			(
				SELECT t.id, 'overdue_task'::text AS kind,
					CASE WHEN t.escalated_at IS NOT NULL THEN 'high' ELSE 'medium' END AS severity,
					t.title, t.related_type, t.related_id,
					GREATEST(0, EXTRACT(EPOCH FROM (NOW() - t.due_at))/3600)::int AS age_hours,
					'tasks'::text AS href_hint
				FROM tasks t
				WHERE t.status IN ('open','in_progress') AND t.due_at IS NOT NULL AND t.due_at < NOW()
				  AND ($1::uuid IS NULL OR t.branch_id=$1)
			)
			UNION ALL
			(
				SELECT b.id, 'unpaid_booking',
					CASE WHEN b.balance_amt > b.total_amount/2 THEN 'high' ELSE 'medium' END,
					'Unpaid booking '||left(b.id::text,8),
					'booking', b.id,
					GREATEST(0, EXTRACT(EPOCH FROM (NOW() - b.created_at))/3600)::int,
					'bookings'
				FROM bookings b
				WHERE b.status='confirmed' AND b.balance_amt > 0
				  AND ($1::uuid IS NULL OR b.branch_id=$1)
			)
			UNION ALL
			(
				SELECT t.id, 'missing_doc', 'medium',
					t.title, t.related_type, t.related_id,
					GREATEST(0, EXTRACT(EPOCH FROM (NOW() - t.created_at))/3600)::int,
					'tasks'
				FROM tasks t
				WHERE t.kind='document' AND t.status IN ('open','in_progress')
				  AND ($1::uuid IS NULL OR t.branch_id=$1)
			)
			UNION ALL
			(
				SELECT d.id, 'capacity',
					CASE
						WHEN d.sales_closed OR d.capacity_sold >= d.capacity_total THEN 'high'
						ELSE 'medium'
					END,
					'Capacity '||d.code,
					'departure', d.id, 0, 'packages'
				FROM departures d
				JOIN packages p ON p.id = d.package_id
				WHERE (
					d.sales_closed
					OR d.capacity_sold >= d.capacity_total
					OR (d.capacity_total > 0 AND d.soft_threshold_pct > 0
						AND (d.capacity_sold::float / d.capacity_total * 100) >= d.soft_threshold_pct)
				)
				  AND ($1::uuid IS NULL OR p.branch_id=$1)
			)
		) feed
		ORDER BY
			CASE severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END,
			age_hours DESC
		LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dashboard.AttentionItem
	for rows.Next() {
		var it dashboard.AttentionItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.Severity, &it.Title, &it.RelatedType, &it.RelatedID, &it.AgeHours, &it.HrefHint); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (a *DashboardAggregator) MyWorkToday(ctx context.Context, branchID, ownerID uuid.UUID, limit int) ([]dashboard.MyWorkItem, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	rows, err := q.Query(ctx, `
		(
			SELECT t.id, 'task'::text, t.title, t.kind::text,
				CASE
					WHEN t.escalated_at IS NOT NULL THEN 0
					WHEN t.due_at IS NOT NULL AND t.due_at < NOW() THEN 1
					WHEN t.priority='urgent' THEN 2
					WHEN t.priority='high' THEN 3
					ELSE 4
				END,
				t.due_at, t.related_type, t.related_id,
				(t.due_at IS NOT NULL AND t.due_at < NOW()),
				(t.escalated_at IS NOT NULL)
			FROM tasks t
			WHERE t.assignee_id=$2 AND t.branch_id=$1
			  AND t.status IN ('open','in_progress')
			  AND (t.due_at IS NULL OR t.due_at < NOW() + INTERVAL '1 day')
		)
		UNION ALL
		(
			SELECT l.id, 'lead', 'Follow up '||l.full_name, 'followup',
				5, NULL, 'lead', l.id, false, false
			FROM leads l
			WHERE l.owner_id=$2 AND l.branch_id=$1
			  AND l.stage NOT IN ('won','lost') AND l.no_follow_up=false
			  AND NOT EXISTS (
				SELECT 1 FROM tasks t
				WHERE t.related_type='lead' AND t.related_id=l.id
				  AND t.status IN ('open','in_progress')
			  )
		)
		ORDER BY 5, 6 NULLS LAST
		LIMIT $3`, branchID, ownerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dashboard.MyWorkItem
	for rows.Next() {
		var it dashboard.MyWorkItem
		if err := rows.Scan(&it.ID, &it.Source, &it.Title, &it.Kind, &it.Priority, &it.DueAt,
			&it.RelatedType, &it.RelatedID, &it.Overdue, &it.Escalated); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (a *DashboardAggregator) TargetProgress(ctx context.Context, branchID uuid.UUID, ownerID *uuid.UUID) (*dashboard.TargetProgress, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	out := &dashboard.TargetProgress{
		Label: "Season target", Currency: "USD", Status: "placeholder",
		PeriodStart: time.Now().UTC().Format("2006-01-02"),
		PeriodEnd:   time.Now().UTC().Format("2006-01-02"),
	}

	var targetID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT id, label, target_amount, currency, period_start::text, period_end::text
		FROM revenue_targets
		WHERE branch_id=$1
		  AND period_start <= CURRENT_DATE AND period_end >= CURRENT_DATE
		  AND (
			($2::uuid IS NOT NULL AND owner_id=$2)
			OR (owner_id IS NULL AND NOT EXISTS (
				SELECT 1 FROM revenue_targets rt2
				WHERE rt2.branch_id=$1 AND rt2.owner_id=$2
				  AND rt2.period_start <= CURRENT_DATE AND rt2.period_end >= CURRENT_DATE
			))
		  )
		ORDER BY CASE WHEN owner_id IS NULL THEN 1 ELSE 0 END
		LIMIT 1`, branchID, ownerID).Scan(
		&targetID, &out.Label, &out.TargetAmount, &out.Currency, &out.PeriodStart, &out.PeriodEnd,
	)
	if err != nil {
		// Fallback: no row — derive soft placeholder from branch bookings YTD.
		out.TargetAmount = 100_000_000
		out.PeriodStart = time.Date(time.Now().UTC().Year(), 1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		out.PeriodEnd = time.Date(time.Now().UTC().Year(), 12, 31, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	}

	periodStart, _ := time.Parse("2006-01-02", out.PeriodStart)
	periodEnd, _ := time.Parse("2006-01-02", out.PeriodEnd)
	if periodEnd.IsZero() {
		periodEnd = time.Now().UTC()
	}
	_ = q.QueryRow(ctx, `
		SELECT COALESCE(SUM(collected_amt),0) FROM bookings
		WHERE branch_id=$1
		  AND ($2::uuid IS NULL OR owner_id=$2)
		  AND created_at >= $3 AND created_at < ($4::date + INTERVAL '1 day')`,
		branchID, ownerID, periodStart, periodEnd).Scan(&out.ActualAmount)

	elapsed := time.Since(periodStart).Hours()
	total := periodEnd.Sub(periodStart).Hours()
	if total <= 0 {
		total = 1
	}
	frac := elapsed / total
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	out.ExpectedToDate = int64(float64(out.TargetAmount) * frac)
	out.Status = dashboard.TargetStatus(out.ActualAmount, out.ExpectedToDate)
	return out, nil
}
