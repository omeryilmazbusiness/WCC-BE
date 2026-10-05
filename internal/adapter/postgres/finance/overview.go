package finance

import (
	"context"
	"time"

	"github.com/google/uuid"

	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
)

func (r *Repository) moneyQuery(ctx context.Context, sql string, w *where) ([]app.Money, error) {
	rows, err := r.q(ctx).Query(ctx, sql, w.args...)
	if err != nil {
		return nil, err
	}
	return collectMoney(rows)
}

func (r *Repository) CashByCurrency(ctx context.Context, branchID *uuid.UUID) ([]app.Money, error) {
	w := newWhere(branchID, "a.branch_id").add("a.is_active")
	if err := w.scope(ctx, branchOnly("a.branch_id")); err != nil {
		return nil, err
	}
	return r.moneyQuery(ctx, `SELECT a.currency, COALESCE(SUM(a.balance),0)::bigint, COUNT(*)::int
		FROM treasury_accounts a WHERE `+w.sql()+` GROUP BY a.currency`, w)
}

func (r *Repository) ReceivablesByCurrency(ctx context.Context, branchID *uuid.UUID) ([]app.Money, error) {
	w := newWhere(branchID, "b.branch_id").add("b.status IN " + activeBookings).add("b.balance_amt > 0")
	if err := w.scope(ctx, ownedBooks); err != nil {
		return nil, err
	}
	return r.moneyQuery(ctx, `SELECT COALESCE(b.currency,'SAR'), COALESCE(SUM(b.balance_amt),0)::bigint, COUNT(*)::int
		FROM bookings b WHERE `+w.sql()+` GROUP BY 1`, w)
}

// PayablesByCurrency is the credit-line debt of postpaid suppliers plus
// unpaid approved/submitted invoices of the other suppliers (a postpaid
// supplier's invoices are already inside its credit line).
func (r *Repository) PayablesByCurrency(ctx context.Context, branchID *uuid.UUID) ([]app.Money, error) {
	w := newWhere(branchID, "s.branch_id").add("s.payment_model='postpaid'").add("s.credit_used > 0")
	if err := w.scope(ctx, branchOnly("s.branch_id")); err != nil {
		return nil, err
	}
	first := w.sql()
	w2 := &where{args: w.args}
	w2.add("(?::uuid IS NULL OR i.branch_id=?)", w.args[0], w.args[0])
	w2.add("i.status IN ('submitted','approved')").add("i.grand_total > 0").add("sx.payment_model <> 'postpaid'")
	if err := w2.scope(ctx, branchOnly("i.branch_id")); err != nil {
		return nil, err
	}
	return r.moneyQuery(ctx, `SELECT cur, COALESCE(SUM(amt),0)::bigint, COUNT(*)::int FROM (
			SELECT s.currency AS cur, s.credit_used AS amt FROM suppliers s WHERE `+first+`
			UNION ALL
			SELECT i.currency, i.grand_total FROM supplier_invoices i JOIN suppliers sx ON sx.id=i.supplier_id
			WHERE `+w2.sql()+`
		) x GROUP BY cur`, w2)
}

func (r *Repository) DepositsByCurrency(ctx context.Context, branchID *uuid.UUID) ([]app.Money, error) {
	w := newWhere(branchID, "s.branch_id").add("s.is_active").add("s.payment_model='prepaid'")
	if err := w.scope(ctx, branchOnly("s.branch_id")); err != nil {
		return nil, err
	}
	return r.moneyQuery(ctx, `SELECT s.currency, COALESCE(SUM(s.deposit_balance),0)::bigint, COUNT(*)::int
		FROM suppliers s WHERE `+w.sql()+` GROUP BY s.currency`, w)
}

func (r *Repository) MonthlyPnL(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]app.PeriodPnL, error) {
	w := newWhere(branchID, "b.branch_id").add("b.status IN "+revenueBookings).add("b.created_at >= ?", from).add("b.created_at < ?", to)
	if err := w.scope(ctx, ownedBooks); err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `
		SELECT date_trunc('month', b.created_at AT TIME ZONE 'UTC')::date, COALESCE(b.currency,'SAR'),
			COALESCE(SUM(b.total_amount - b.tax_amt),0)::bigint,
			COALESCE(SUM(b.total_amount) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COALESCE(SUM(b.cost_amt) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COALESCE(SUM(b.tax_amt) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COALESCE(SUM(b.fee_amt) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COUNT(*) FILTER (WHERE b.cost_amt > 0)::int,
			COUNT(*)::int
		FROM bookings b WHERE `+w.sql()+` GROUP BY 1, 2`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []app.PeriodPnL
	for rows.Next() {
		var p app.PeriodPnL
		var costed int
		if err := rows.Scan(&p.Month, &p.Currency, &p.Revenue, &p.PnL.Gross, &p.PnL.Net, &p.PnL.Tax, &p.PnL.Fee, &costed, &p.Bookings); err != nil {
			return nil, err
		}
		p.PnL.Costed = costed > 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) AlertCounts(ctx context.Context, branchID *uuid.UUID, today time.Time) (app.AlertCounts, error) {
	var c app.AlertCounts
	type count struct {
		dst   *int
		sql   string
		alias string
		cols  string
		extra []any
	}
	counts := []count{
		{&c.LowDeposits, `SELECT COUNT(*) FROM suppliers s WHERE %s AND s.is_active AND s.low_balance_threshold > 0 AND (
			(s.payment_model='prepaid' AND s.deposit_balance < s.low_balance_threshold) OR
			(s.payment_model='postpaid' AND s.credit_limit > 0 AND s.credit_limit - s.credit_used < s.low_balance_threshold))`, "s", "s.branch_id", nil},
		{&c.LowAccounts, `SELECT COUNT(*) FROM treasury_accounts a WHERE %s AND a.is_active
			AND a.low_balance_threshold > 0 AND a.balance < a.low_balance_threshold`, "a", "a.branch_id", nil},
		{&c.UnmatchedCredits, `SELECT COUNT(*) FROM treasury_movements m WHERE %s AND m.match_status='unmatched'`, "m", "m.branch_id", nil},
		{&c.PendingRefunds, `SELECT COUNT(*) FROM payments p JOIN bookings b ON b.id=p.booking_id
			WHERE %s AND p.event_type='refund' AND p.status='pending_approval'`, "b", "b.branch_id", nil},
		{&c.SuspendedAgencies, `SELECT COUNT(*) FROM agencies g WHERE %s AND g.status='suspended'`, "g", "g.branch_id", nil},
		{&c.OverdueSchedules, `SELECT COUNT(*) FROM payment_schedules ps JOIN bookings b ON b.id=ps.booking_id
			WHERE %s AND ps.status IN ('open','overdue') AND ps.due_at::date < ?`, "b", "b.branch_id", []any{today}},
		{&c.SupplierDueSoon, `SELECT COUNT(*) FROM supplier_invoices i WHERE %s AND i.status='approved'
			AND i.due_on IS NOT NULL AND i.due_on <= ?`, "i", "i.branch_id", []any{today.AddDate(0, 0, 7)}},
	}
	for _, k := range counts {
		w := newWhere(branchID, k.cols)
		if err := w.scope(ctx, branchOnly(k.cols)); err != nil {
			return c, err
		}
		cond := w.sql()
		sql := sprintf(k.sql, cond)
		for _, v := range k.extra {
			w.args = append(w.args, v)
			sql = replaceFirst(sql, "?", placeholder(len(w.args)))
		}
		if err := r.q(ctx).QueryRow(ctx, sql, w.args...).Scan(k.dst); err != nil {
			return c, err
		}
	}
	return c, nil
}

// dueCTE spreads every open booking balance over due dates: open payment
// schedules take the balance latest-first (payments settle the earliest
// instalments), whatever is left is due on the agency terms date or is
// unscheduled (NULL). Columns: booking_id, currency, agency_id, due_on, amt.
func dueCTE(openWhere string) string {
	return `WITH open_b AS (
			SELECT b.id, b.branch_id, COALESCE(b.currency,'SAR') AS currency, b.balance_amt, b.created_at, b.agency_id,
				b.customer_id, b.ref_no
			FROM bookings b WHERE ` + openWhere + `
		), sched AS (
			SELECT ps.id, ps.booking_id, ps.due_at::date AS due_on, ps.amount,
				SUM(ps.amount) OVER (PARTITION BY ps.booking_id ORDER BY ps.due_at DESC, ps.id) AS cum
			FROM payment_schedules ps JOIN open_b o ON o.id = ps.booking_id
			WHERE ps.status IN ('open','overdue')
		), alloc AS (
			SELECT sc.booking_id, sc.due_on, LEAST(sc.amount, GREATEST(o.balance_amt - (sc.cum - sc.amount), 0)) AS amt
			FROM sched sc JOIN open_b o ON o.id = sc.booking_id
		), dues AS (
			SELECT o.id AS booking_id, o.currency, o.agency_id, al.due_on, al.amt
			FROM alloc al JOIN open_b o ON o.id = al.booking_id WHERE al.amt > 0
			UNION ALL
			SELECT o.id, o.currency, o.agency_id,
				CASE WHEN o.agency_id IS NOT NULL THEN o.created_at::date + ag.payment_terms_days END,
				o.balance_amt - COALESCE((SELECT SUM(al.amt) FROM alloc al WHERE al.booking_id = o.id), 0)
			FROM open_b o LEFT JOIN agencies ag ON ag.id = o.agency_id
			WHERE o.balance_amt - COALESCE((SELECT SUM(al.amt) FROM alloc al WHERE al.booking_id = o.id), 0) > 0
		)`
}

func (r *Repository) Ageing(ctx context.Context, branchID *uuid.UUID, today time.Time) ([]*domain.Ageing, error) {
	w := newWhere(branchID, "b.branch_id").add("b.status IN " + activeBookings).add("b.balance_amt > 0")
	if err := w.scope(ctx, ownedBooks); err != nil {
		return nil, err
	}
	w.args = append(w.args, today)
	t := placeholder(len(w.args))
	rows, err := r.q(ctx).Query(ctx, dueCTE(w.sql())+`
		SELECT currency,
			CASE WHEN due_on IS NULL THEN 'unscheduled'
				WHEN due_on >= `+t+`::date THEN 'current'
				WHEN `+t+`::date - due_on <= 15 THEN 'd0_15'
				WHEN `+t+`::date - due_on <= 30 THEN 'd16_30'
				ELSE 'd31_plus' END,
			SUM(amt)::bigint, COUNT(*)::int
		FROM dues GROUP BY 1, 2 ORDER BY 1`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCur := map[string]*domain.Ageing{}
	var out []*domain.Ageing
	for rows.Next() {
		var cur, bucket string
		var amt int64
		var n int
		if err := rows.Scan(&cur, &bucket, &amt, &n); err != nil {
			return nil, err
		}
		a, ok := byCur[cur]
		if !ok {
			a = domain.NewAgeing(cur)
			byCur[cur] = a
			out = append(out, a)
		}
		a.Buckets[bucket] += amt
		a.Counts[bucket] += n
	}
	return out, rows.Err()
}

func (r *Repository) TopDebtors(ctx context.Context, branchID *uuid.UUID, today time.Time, limit int) ([]app.Debtor, error) {
	w := newWhere(branchID, "b.branch_id").add("b.status IN " + activeBookings).add("b.balance_amt > 0")
	if err := w.scope(ctx, ownedBooks); err != nil {
		return nil, err
	}
	w.args = append(w.args, today, limit)
	t, lim := placeholder(len(w.args)-1), placeholder(len(w.args))
	rows, err := r.q(ctx).Query(ctx, dueCTE(w.sql())+`
		SELECT o.id, o.ref_no, COALESCE(c.full_name,''), COALESCE(c.phone,''), o.agency_id, COALESCE(ag.name,''),
			o.currency, o.balance_amt, d.first_due
		FROM open_b o
		LEFT JOIN customers c ON c.id = o.customer_id
		LEFT JOIN agencies ag ON ag.id = o.agency_id
		LEFT JOIN LATERAL (SELECT MIN(due_on) AS first_due FROM dues WHERE dues.booking_id = o.id) d ON TRUE
		ORDER BY (d.first_due IS NOT NULL AND d.first_due < `+t+`::date) DESC, o.balance_amt DESC
		LIMIT `+lim, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.Debtor{}
	for rows.Next() {
		var d app.Debtor
		if err := rows.Scan(&d.BookingID, &d.RefNo, &d.CustomerName, &d.Phone, &d.AgencyID, &d.AgencyName,
			&d.Currency, &d.Balance, &d.DueOn); err != nil {
			return nil, err
		}
		if d.DueOn != nil {
			d.DaysLate = max(domain.DaysBetween(*d.DueOn, today), 0)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
