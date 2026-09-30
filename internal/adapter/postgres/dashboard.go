package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	"github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var (
	dashLeads    = pgscope.Columns{Branch: "l.branch_id", Owner: "l.owner_id"}
	dashTasks    = pgscope.Columns{Branch: "t.branch_id", Owner: "t.assignee_id"}
	dashBookings = pgscope.Columns{Branch: "b.branch_id", Owner: "b.owner_id"}
	dashPackages = pgscope.Columns{Branch: "p.branch_id"}
)

// dashScopes renders one scope clause per column set, all bound into args,
// so aggregate queries count only records visible to the caller.
func dashScopes(ctx context.Context, args []any, cols ...pgscope.Columns) ([]string, []any, error) {
	out := make([]string, len(cols))
	for i, c := range cols {
		var err error
		if out[i], args, err = pgscope.Clause(ctx, c, args); err != nil {
			return nil, args, err
		}
	}
	return out, args, nil
}

type DashboardAggregator struct {
	pool *pgxpool.Pool
}

func NewDashboardAggregator(pool *pgxpool.Pool) *DashboardAggregator {
	return &DashboardAggregator{pool: pool}
}

func (a *DashboardAggregator) Compute(ctx context.Context, branchID *uuid.UUID, from, to time.Time) (*dashboard.KPI, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	kpi := &dashboard.KPI{PeriodFrom: from, PeriodTo: to}
	// Each statement below reads one table, so each binds only its own
	// scope placeholders on top of the shared $1..$3.
	base := []any{from, to, branchID}
	lsc, largs, err := pgscope.Clause(ctx, dashLeads, append([]any(nil), base...))
	if err != nil {
		return nil, err
	}
	tsc, targs, err := pgscope.Clause(ctx, dashTasks, append([]any(nil), base...))
	if err != nil {
		return nil, err
	}
	bsc, bargs, err := pgscope.Clause(ctx, dashBookings, append([]any(nil), base...))
	if err != nil {
		return nil, err
	}

	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM leads l
		WHERE l.stage NOT IN ('won','lost')
		  AND l.created_at >= $1 AND l.created_at < $2
		  AND ($3::uuid IS NULL OR l.branch_id = $3)`+lsc, largs...).Scan(&kpi.LeadsOpen); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks t
		WHERE t.status IN ('open','in_progress')
		  AND t.due_at IS NOT NULL AND t.due_at < NOW()
		  AND t.due_at >= $1 AND t.due_at < $2
		  AND ($3::uuid IS NULL OR t.branch_id = $3)`+tsc, targs...).Scan(&kpi.TasksOverdue); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM bookings b
		WHERE b.balance_amt > 0 AND b.status IN ('confirmed','partially_paid','ready','travelled')
		  AND b.created_at >= $1 AND b.created_at < $2
		  AND ($3::uuid IS NULL OR b.branch_id = $3)`+bsc, bargs...).Scan(&kpi.BookingsUnpaid); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks t
		WHERE t.kind = 'document' AND t.status IN ('open','in_progress')
		  AND t.created_at >= $1 AND t.created_at < $2
		  AND ($3::uuid IS NULL OR t.branch_id = $3)`+tsc, targs...).Scan(&kpi.MissingDocs); err != nil {
		return nil, err
	}
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(b.total_amount),0), COALESCE(SUM(b.collected_amt),0),
			COALESCE(SUM(b.total_amount - b.tax_amt - b.fee_amt - COALESCE(b.cost_amt,0)),0)
		FROM bookings b
		WHERE b.status IN ('confirmed','partially_paid','ready','travelled','completed')
		  AND b.created_at >= $1 AND b.created_at < $2
		  AND ($3::uuid IS NULL OR b.branch_id = $3)`+bsc, bargs...).
		Scan(&kpi.BookedAmt, &kpi.CollectedAmt, &kpi.MarginAmt); err != nil {
		return nil, err
	}
	return kpi, nil
}

func (a *DashboardAggregator) TeamPerformance(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]dashboard.TeamMember, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	sc, args, err := dashScopes(ctx, []any{from, to, branchID}, dashLeads, dashTasks, dashBookings)
	if err != nil {
		return nil, err
	}
	lsc, tsc, bsc := sc[0], sc[1], sc[2]
	rows, err := q.Query(ctx, `
		WITH owners AS (
			SELECT DISTINCT l.owner_id AS id FROM leads l
			WHERE l.created_at >= $1 AND l.created_at < $2 AND ($3::uuid IS NULL OR l.branch_id=$3)`+lsc+`
			UNION
			SELECT DISTINCT t.assignee_id FROM tasks t
			WHERE t.created_at >= $1 AND t.created_at < $2 AND ($3::uuid IS NULL OR t.branch_id=$3)`+tsc+`
			UNION
			SELECT DISTINCT b.owner_id FROM bookings b
			WHERE b.created_at >= $1 AND b.created_at < $2 AND ($3::uuid IS NULL OR b.branch_id=$3)`+bsc+`
		)
		SELECT o.id,
			COALESCE(u.full_name, ''),
			COALESCE(u.role, ''),
			(SELECT COUNT(*) FROM leads l WHERE l.owner_id=o.id AND l.created_at >= $1 AND l.created_at < $2 AND ($3::uuid IS NULL OR l.branch_id=$3)`+lsc+`),
			(SELECT COUNT(*) FROM leads l WHERE l.owner_id=o.id AND l.stage='won' AND l.updated_at >= $1 AND l.updated_at < $2 AND ($3::uuid IS NULL OR l.branch_id=$3)`+lsc+`),
			(SELECT COUNT(*) FROM tasks t WHERE t.assignee_id=o.id AND t.status IN ('open','in_progress') AND ($3::uuid IS NULL OR t.branch_id=$3)`+tsc+`),
			(SELECT COUNT(*) FROM tasks t WHERE t.assignee_id=o.id AND t.status IN ('open','in_progress') AND t.due_at < NOW() AND ($3::uuid IS NULL OR t.branch_id=$3)`+tsc+`)
		FROM owners o
		LEFT JOIN users u ON u.id = o.id
		WHERE o.id IS NOT NULL
		ORDER BY 4 DESC, 5 DESC, 2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []dashboard.TeamMember
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var m dashboard.TeamMember
		if err := rows.Scan(&m.OwnerID, &m.OwnerName, &m.Role, &m.LeadsHandled, &m.LeadsWon, &m.OpenTasks, &m.OverdueTasks); err != nil {
			return nil, err
		}
		index[m.OwnerID] = len(out)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}

	bookingScope, bookingArgs, err := dashScopes(ctx, []any{from, to, branchID}, dashBookings)
	if err != nil {
		return nil, err
	}
	money, err := q.Query(ctx, `
		SELECT b.owner_id, b.currency, COALESCE(SUM(b.collected_amt), 0)::bigint, COUNT(*)
		FROM bookings b
		WHERE b.owner_id IS NOT NULL AND b.created_at >= $1 AND b.created_at < $2 AND ($3::uuid IS NULL OR b.branch_id=$3)`+bookingScope[0]+`
		GROUP BY 1, 2`, bookingArgs...)
	if err != nil {
		return nil, err
	}
	defer money.Close()
	for money.Next() {
		var owner uuid.UUID
		var amt dashboard.Amount
		if err := money.Scan(&owner, &amt.Currency, &amt.Minor, &amt.Count); err != nil {
			return nil, err
		}
		if i, ok := index[owner]; ok {
			out[i].Collected = append(out[i].Collected, amt)
		}
	}
	return out, money.Err()
}

func (a *DashboardAggregator) MyWorkToday(ctx context.Context, branchID *uuid.UUID, ownerID uuid.UUID, limit int) ([]dashboard.MyWorkItem, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	sc, args, err := dashScopes(ctx, []any{branchID, ownerID}, dashTasks, dashLeads)
	if err != nil {
		return nil, err
	}
	tsc, lsc := sc[0], sc[1]
	args = append(args, limit)
	rows, err := q.Query(ctx, fmt.Sprintf(`
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
			WHERE t.assignee_id=$2 AND ($1::uuid IS NULL OR t.branch_id=$1)
			  AND t.status IN ('open','in_progress')
			  AND (t.due_at IS NULL OR t.due_at < NOW() + INTERVAL '1 day')`+tsc+`
		)
		UNION ALL
		(
			SELECT l.id, 'lead', 'Follow up '||l.full_name, 'followup',
				5, NULL, 'lead', l.id, false, false
			FROM leads l
			WHERE l.owner_id=$2 AND ($1::uuid IS NULL OR l.branch_id=$1)
			  AND l.stage NOT IN ('won','lost') AND l.no_follow_up=false`+lsc+`
			  AND NOT EXISTS (
				SELECT 1 FROM tasks t
				WHERE t.related_type='lead' AND t.related_id=l.id
				  AND t.status IN ('open','in_progress')
			  )
		)
		ORDER BY 5, 6 NULLS LAST
		LIMIT $%d`, len(args)), args...)
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
	today := time.Now().UTC().Format("2006-01-02")
	out := &dashboard.TargetProgress{Status: "placeholder", PeriodStart: today, PeriodEnd: today}

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
	if errors.Is(err, pgx.ErrNoRows) {
		// No running target: report an honest placeholder instead of inventing a goal.
		return out, nil
	}
	if err != nil {
		return nil, err
	}

	periodStart, _ := time.Parse("2006-01-02", out.PeriodStart)
	periodEnd, _ := time.Parse("2006-01-02", out.PeriodEnd)
	if periodEnd.IsZero() {
		periodEnd = time.Now().UTC()
	}
	bsc, args, err := pgscope.Clause(ctx, dashBookings, []any{branchID, ownerID, periodStart, periodEnd})
	if err != nil {
		return nil, err
	}
	_ = q.QueryRow(ctx, `
		SELECT COALESCE(SUM(b.collected_amt),0) FROM bookings b
		WHERE b.branch_id=$1
		  AND ($2::uuid IS NULL OR b.owner_id=$2)
		  AND b.created_at >= $3 AND b.created_at < ($4::date + INTERVAL '1 day')`+bsc,
		args...).Scan(&out.ActualAmount)

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
