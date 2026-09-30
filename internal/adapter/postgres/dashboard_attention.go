package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// attentionTask is shared by the three task-backed kinds: it resolves who the
// task is about and which record the row should open.
const attentionTask = `
	SELECT t.id, %[1]s AS kind, %[2]s AS severity, t.title, t.related_type, t.related_id,
		GREATEST(0, EXTRACT(EPOCH FROM (NOW() - %[3]s))/3600)::int AS age_hours,
		'tasks'::text AS href_hint,
		COALESCE(tc.full_name, tl.full_name, tr.label, NULLIF(cvc.full_name, ''), NULLIF(cvi.display_name, ''), cvi.phone, '') AS context,
		CASE WHEN t.related_type IN ('booking','lead','conversation','revenue_target') THEN t.related_type ELSE 'task' END AS link_type,
		CASE WHEN t.related_type IN ('booking','lead','conversation','revenue_target') THEN t.related_id ELSE t.id END AS link_id,
		NULL::bigint AS amount, ''::text AS currency, NULL::int AS capacity_sold, NULL::int AS capacity_total,
		t.due_at
	FROM tasks t
	LEFT JOIN bookings tb ON t.related_type = 'booking' AND tb.id = t.related_id
	LEFT JOIN customers tc ON tc.id = tb.customer_id
	LEFT JOIN leads tl ON t.related_type = 'lead' AND tl.id = t.related_id
	LEFT JOIN revenue_targets tr ON t.related_type = 'revenue_target' AND tr.id = t.related_id
	LEFT JOIN conversations cv ON t.related_type = 'conversation' AND cv.id = t.related_id
	LEFT JOIN customers cvc ON cvc.id = cv.customer_id
	LEFT JOIN channel_identities cvi ON cvi.id = cv.channel_identity_id
	WHERE t.status IN ('open','in_progress') AND ($1::uuid IS NULL OR t.branch_id = $1)`

// attentionFeedSQL is every open exception, one row per record, in the column
// order scanned by scanAttention. tsc/bsc/psc are the task/booking/package scopes.
func attentionFeedSQL(tsc, bsc, psc string) string {
	escalated := fmt.Sprintf(attentionTask, `'escalated_task'::text`, `'high'::text`, `t.escalated_at`) +
		` AND t.escalated_at IS NOT NULL` + tsc
	overdue := fmt.Sprintf(attentionTask, `'overdue_task'`,
		`CASE WHEN t.due_at < NOW() - INTERVAL '3 days' THEN 'high' ELSE 'medium' END`, `t.due_at`) +
		` AND t.escalated_at IS NULL AND t.kind <> 'document' AND t.due_at IS NOT NULL AND t.due_at < NOW()` + tsc
	missingDoc := fmt.Sprintf(attentionTask, `'missing_doc'`,
		`CASE WHEN t.due_at IS NOT NULL AND t.due_at < NOW() THEN 'high' ELSE 'medium' END`, `t.created_at`) +
		` AND t.escalated_at IS NULL AND t.kind = 'document'` + tsc
	return `
		(` + escalated + `)
		UNION ALL (` + overdue + `)
		UNION ALL (` + missingDoc + `)
		UNION ALL (
			SELECT b.id, 'unpaid_booking',
				CASE WHEN ps.first_overdue IS NOT NULL THEN 'high' ELSE 'medium' END,
				c.full_name, 'booking', b.id,
				GREATEST(0, EXTRACT(EPOCH FROM (NOW() - COALESCE(ps.first_overdue, b.created_at)))/3600)::int,
				'bookings',
				p.name_en || ' · ' || d.code,
				'booking', b.id,
				b.balance_amt, b.currency, NULL::int, NULL::int,
				ps.next_due
			FROM bookings b
			JOIN customers c ON c.id = b.customer_id
			JOIN departures d ON d.id = b.departure_id
			JOIN packages p ON p.id = d.package_id
			LEFT JOIN LATERAL (
				SELECT MIN(s.due_at) FILTER (WHERE s.due_at < NOW()) AS first_overdue, MIN(s.due_at) AS next_due
				FROM payment_schedules s
				WHERE s.booking_id = b.id AND s.status IN ('open','overdue')
			) ps ON TRUE
			WHERE b.status IN ('confirmed','partially_paid','ready','travelled') AND b.balance_amt > 0
			  AND (ps.next_due IS NULL OR ps.next_due < NOW() + INTERVAL '7 days')
			  AND ($1::uuid IS NULL OR b.branch_id = $1)` + bsc + `
		)
		UNION ALL (
			SELECT d.id, 'capacity',
				CASE WHEN d.sales_closed OR d.capacity_sold >= d.capacity_total THEN 'high' ELSE 'medium' END,
				p.name_en, 'departure', d.id,
				0, 'packages',
				d.code,
				'package', p.id,
				NULL::bigint, '', d.capacity_sold, d.capacity_total,
				d.depart_date::timestamptz
			FROM departures d
			JOIN packages p ON p.id = d.package_id
			WHERE d.is_active AND d.depart_date >= CURRENT_DATE
			  AND (
				d.sales_closed
				OR d.capacity_sold >= d.capacity_total
				OR (d.capacity_total > 0 AND d.soft_threshold_pct > 0
					AND (d.capacity_sold::float / d.capacity_total * 100) >= d.soft_threshold_pct)
			  )
			  AND ($1::uuid IS NULL OR p.branch_id = $1)` + psc + `
		)`
}

func (a *DashboardAggregator) attentionScopes(ctx context.Context, branchID *uuid.UUID) (string, []any, error) {
	sc, args, err := dashScopes(ctx, []any{branchID}, dashTasks, dashBookings, dashPackages)
	if err != nil {
		return "", nil, err
	}
	return attentionFeedSQL(sc[0], sc[1], sc[2]), args, nil
}

// AttentionFeed returns the most urgent open exceptions: high severity first, then oldest.
func (a *DashboardAggregator) AttentionFeed(ctx context.Context, branchID *uuid.UUID, limit int) ([]dashboard.AttentionItem, error) {
	feed, args, err := a.attentionScopes(ctx, branchID)
	if err != nil {
		return nil, err
	}
	args = append(args, limit)
	rows, err := tx.QuerierFrom(ctx, a.pool).Query(ctx, fmt.Sprintf(`
		SELECT * FROM (%s) feed
		ORDER BY CASE severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END, age_hours DESC, id
		LIMIT $%d`, feed, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []dashboard.AttentionItem{}
	for rows.Next() {
		var it dashboard.AttentionItem
		if err := rows.Scan(
			&it.ID, &it.Kind, &it.Severity, &it.Title, &it.RelatedType, &it.RelatedID, &it.AgeHours, &it.HrefHint,
			&it.Context, &it.LinkType, &it.LinkID, &it.Amount, &it.Currency, &it.CapacitySold, &it.CapacityTotal, &it.DueAt,
		); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AttentionSummary counts the whole feed by kind and severity.
func (a *DashboardAggregator) AttentionSummary(ctx context.Context, branchID *uuid.UUID) (*dashboard.AttentionSummary, error) {
	feed, args, err := a.attentionScopes(ctx, branchID)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, a.pool).Query(ctx, `
		SELECT kind, COUNT(*), COUNT(*) FILTER (WHERE severity = 'high')
		FROM (`+feed+`) feed
		GROUP BY kind`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &dashboard.AttentionSummary{Kinds: map[string]int{}}
	for rows.Next() {
		var kind string
		var n, high int
		if err := rows.Scan(&kind, &n, &high); err != nil {
			return nil, err
		}
		out.Kinds[kind] = n
		out.Total += n
		out.High += high
	}
	return out, rows.Err()
}
