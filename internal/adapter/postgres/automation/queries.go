// Package automation holds the cross-module read models scheduled automation
// sweeps need (branch clocks, candidates for reminders).
package automation

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	appautomation "github.com/wodi-crm/wodi-crm-be/internal/app/automation"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Default thresholds when a branch has no alert_threshold_settings row; they
// mirror adminconfig.DefaultAlertThresholds.
const (
	defaultMissingDocHours     = 24
	defaultLeadIdleHours       = 24
	defaultPaymentOverdueHours = 12
	// overdueLookback bounds the sweep; older schedules were alerted already.
	overdueLookback = 30 * 24 * time.Hour
)

type Queries struct {
	pool *pgxpool.Pool
}

func NewQueries(pool *pgxpool.Pool) *Queries { return &Queries{pool: pool} }

func (q *Queries) Branches(ctx context.Context) ([]appautomation.Branch, error) {
	rows, err := tx.QuerierFrom(ctx, q.pool).Query(ctx,
		`SELECT id, timezone FROM branches WHERE is_active ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appautomation.Branch
	for rows.Next() {
		var b appautomation.Branch
		if err := rows.Scan(&b.ID, &b.TimeZone); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// BookingsAwaitingDocs lists confirmed, not yet departed bookings whose
// confirmation is older than the branch missing-document window.
func (q *Queries) BookingsAwaitingDocs(ctx context.Context, now time.Time, limit int) ([]appautomation.BookingRef, error) {
	rows, err := tx.QuerierFrom(ctx, q.pool).Query(ctx, `
		SELECT b.id, b.branch_id, b.owner_id, d.code, d.depart_date
		FROM bookings b
		JOIN departures d ON d.id = b.departure_id
		LEFT JOIN alert_threshold_settings ats ON ats.branch_id = b.branch_id
		WHERE b.status IN ('confirmed', 'partially_paid')
		  AND d.depart_date >= ($1::timestamptz)::date
		  AND COALESCE(b.status_changed_at, b.created_at)
		      <= $1 - make_interval(hours => COALESCE(ats.missing_doc_hours, $2))
		ORDER BY d.depart_date, b.id
		LIMIT $3`, now, defaultMissingDocHours, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appautomation.BookingRef
	for rows.Next() {
		var b appautomation.BookingRef
		if err := rows.Scan(&b.ID, &b.BranchID, &b.OwnerID, &b.DepartureCode, &b.DepartDate); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// IdleLeads lists open leads without an open task that nobody touched within
// the branch follow-up window.
func (q *Queries) IdleLeads(ctx context.Context, now time.Time, limit int) ([]appautomation.LeadRef, error) {
	rows, err := tx.QuerierFrom(ctx, q.pool).Query(ctx, `
		SELECT l.id, l.branch_id, l.owner_id, l.full_name, l.updated_at
		FROM leads l
		LEFT JOIN alert_threshold_settings ats ON ats.branch_id = l.branch_id
		WHERE l.stage NOT IN ('won', 'lost')
		  AND NOT l.no_follow_up
		  AND l.updated_at <= $1 - make_interval(hours => COALESCE(ats.lead_no_followup_hours, $2))
		  AND NOT EXISTS (
		      SELECT 1 FROM tasks t
		      WHERE t.related_type = 'lead' AND t.related_id = l.id
		        AND t.status IN ('open', 'in_progress'))
		ORDER BY l.updated_at, l.id
		LIMIT $3`, now, defaultLeadIdleHours, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appautomation.LeadRef
	for rows.Next() {
		var l appautomation.LeadRef
		if err := rows.Scan(&l.ID, &l.BranchID, &l.OwnerID, &l.FullName, &l.IdleSince); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ExpiringPassports lists travelling customers (an upcoming, not cancelled
// booking) whose passport expires by horizon; the recipient is the owner of
// the nearest such booking.
func (q *Queries) ExpiringPassports(ctx context.Context, today, horizon time.Time, limit int) ([]appautomation.PassportRef, error) {
	rows, err := tx.QuerierFrom(ctx, q.pool).Query(ctx, `
		SELECT c.id, c.branch_id, c.full_name, c.passport_expires_at, nb.owner_id, nb.id
		FROM customers c
		JOIN LATERAL (
		    SELECT b.id, b.owner_id
		    FROM bookings b JOIN departures d ON d.id = b.departure_id
		    WHERE b.customer_id = c.id
		      AND b.status NOT IN ('cancelled', 'travelled', 'completed')
		      AND d.depart_date >= $1::date
		    ORDER BY d.depart_date
		    LIMIT 1
		) nb ON TRUE
		WHERE c.passport_expires_at IS NOT NULL
		  AND c.passport_expires_at <= $2::date
		  AND c.is_active AND c.merged_into_id IS NULL AND c.anonymized_at IS NULL
		ORDER BY c.passport_expires_at, c.id
		LIMIT $3`, today, horizon, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appautomation.PassportRef
	for rows.Next() {
		var p appautomation.PassportRef
		if err := rows.Scan(&p.CustomerID, &p.BranchID, &p.FullName, &p.ExpiresAt, &p.OwnerID, &p.BookingID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OverdueSchedules lists unpaid schedules of live bookings that are past due
// by more than the branch payment_overdue_hours.
func (q *Queries) OverdueSchedules(ctx context.Context, now time.Time, limit int) ([]appautomation.OverdueScheduleRef, error) {
	rows, err := tx.QuerierFrom(ctx, q.pool).Query(ctx, `
		SELECT s.id, s.booking_id, b.branch_id, b.owner_id, s.due_at, s.amount, s.currency
		FROM payment_schedules s
		JOIN bookings b ON b.id = s.booking_id
		LEFT JOIN alert_threshold_settings ats ON ats.branch_id = b.branch_id
		WHERE s.status IN ('open', 'overdue')
		  AND b.status NOT IN ('cancelled', 'draft')
		  AND s.due_at >= $4
		  AND s.due_at <= $1 - make_interval(hours => COALESCE(ats.payment_overdue_hours, $2))
		ORDER BY s.due_at, s.id
		LIMIT $3`, now, defaultPaymentOverdueHours, limit, now.Add(-overdueLookback))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []appautomation.OverdueScheduleRef
	for rows.Next() {
		var s appautomation.OverdueScheduleRef
		if err := rows.Scan(&s.ScheduleID, &s.BookingID, &s.BranchID, &s.OwnerID, &s.DueAt, &s.Amount, &s.Currency); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
