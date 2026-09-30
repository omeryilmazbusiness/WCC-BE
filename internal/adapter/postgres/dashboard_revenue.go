package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	"github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var _ dashboard.RevenueReader = (*DashboardAggregator)(nil)

// Every statement binds $1 branch and $2 reporting currency, then its own
// $3/$4, then the caller's scope placeholders.
const (
	// Booking amounts use the booking's FX snapshot when it is in the
	// reporting currency; otherwise they stay in the booking currency.
	bookingCur = `CASE WHEN b.reporting_currency = $2 AND b.fx_rate_scaled IS NOT NULL THEN $2 ELSE COALESCE(b.currency, 'SAR') END`
	// Payments carry a per-row snapshot frozen at write time.
	paymentCur = `CASE WHEN p.reporting_currency = $2 AND p.amount_reporting IS NOT NULL THEN $2 ELSE p.currency END`
	paymentAmt = `CASE WHEN p.reporting_currency = $2 AND p.amount_reporting IS NOT NULL THEN p.amount_reporting ELSE p.amount END`

	activeBookings  = `('confirmed','partially_paid','ready','travelled')`
	revenueBookings = `('confirmed','partially_paid','ready','travelled','completed')`
	countedPayment  = `p.status IN ('verified','approved')`
	inPeriodDays    = `p.received_at >= $3::date AND p.received_at <= $4::date`
)

func bookingAmt(expr string) string {
	return `CASE WHEN b.reporting_currency = $2 AND b.fx_rate_scaled IS NOT NULL
		THEN ROUND((` + expr + `)::numeric * b.fx_rate_scaled / 100000000)::bigint
		ELSE (` + expr + `) END`
}

func (a *DashboardAggregator) RevenueFacts(ctx context.Context, rq dashboard.RevenueQuery) (*dashboard.RevenueFacts, error) {
	q := tx.QuerierFrom(ctx, a.pool)
	f := &dashboard.RevenueFacts{}
	// bind returns the scoped WHERE prefix and args for one statement.
	bind := func(extra ...any) (string, []any, error) {
		args := append([]any{rq.BranchID, rq.Currency}, extra...)
		scope, args, err := pgscope.Clause(ctx, dashBookings, args)
		return ` WHERE b.branch_id = $1` + scope, args, err
	}
	period := []any{rq.From, rq.To}
	days := []any{dayOf(rq.From), dayOf(rq.To)}

	where, args, err := bind(period...)
	if err != nil {
		return nil, err
	}
	if err := eachRow(ctx, q, `
		SELECT `+bookingCur+`,
			COALESCE(SUM(`+bookingAmt("b.total_amount")+`), 0),
			COALESCE(SUM(`+bookingAmt("b.collected_amt")+`), 0),
			COALESCE(SUM(`+bookingAmt("b.total_amount - b.tax_amt - b.fee_amt - b.cost_amt")+`) FILTER (WHERE b.cost_amt > 0), 0),
			COALESCE(SUM(`+bookingAmt("b.total_amount")+`) FILTER (WHERE b.cost_amt > 0), 0),
			COUNT(*) FILTER (WHERE b.cost_amt > 0),
			COUNT(*)
		FROM bookings b`+where+`
		  AND b.status IN `+revenueBookings+`
		  AND b.created_at >= $3 AND b.created_at < $4
		GROUP BY 1`, args, func(r pgx.Rows) error {
		var cur string
		var total, collected, margin, costedTotal int64
		var costed, n int
		if err := r.Scan(&cur, &total, &collected, &margin, &costedTotal, &costed, &n); err != nil {
			return err
		}
		f.Booked = append(f.Booked, dashboard.Amount{Currency: cur, Minor: total, Count: n})
		f.BookedCollected = append(f.BookedCollected, dashboard.Amount{Currency: cur, Minor: collected})
		f.Margin = append(f.Margin, dashboard.Amount{Currency: cur, Minor: margin, Count: costed})
		f.CostedBooked = append(f.CostedBooked, dashboard.Amount{Currency: cur, Minor: costedTotal, Count: costed})
		return nil
	}); err != nil {
		return nil, err
	}

	if where, args, err = bind(days...); err != nil {
		return nil, err
	}
	if err := eachRow(ctx, q, `
		SELECT `+paymentCur+`,
			COALESCE(SUM(`+paymentAmt+`) FILTER (WHERE `+countedPayment+` AND p.event_type <> 'refund' AND `+inPeriodDays+`), 0),
			COUNT(*) FILTER (WHERE `+countedPayment+` AND p.event_type = 'charge' AND `+inPeriodDays+`),
			COALESCE(-SUM(`+paymentAmt+`) FILTER (WHERE p.status = 'approved' AND p.event_type = 'refund' AND `+inPeriodDays+`), 0),
			COUNT(*) FILTER (WHERE p.status = 'approved' AND p.event_type = 'refund' AND `+inPeriodDays+`),
			COALESCE(SUM(`+paymentAmt+`) FILTER (WHERE p.status = 'unverified' AND p.event_type = 'charge'), 0),
			COUNT(*) FILTER (WHERE p.status = 'unverified' AND p.event_type = 'charge')
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id`+where+`
		GROUP BY 1`, args, func(r pgx.Rows) error {
		var cur string
		var collected, refunds, pending int64
		var nCollected, nRefunds, nPending int
		if err := r.Scan(&cur, &collected, &nCollected, &refunds, &nRefunds, &pending, &nPending); err != nil {
			return err
		}
		f.Collected = append(f.Collected, dashboard.Amount{Currency: cur, Minor: collected, Count: nCollected})
		f.Refunds = append(f.Refunds, dashboard.Amount{Currency: cur, Minor: refunds, Count: nRefunds})
		f.Pending = append(f.Pending, dashboard.Amount{Currency: cur, Minor: pending, Count: nPending})
		return nil
	}); err != nil {
		return nil, err
	}

	methods := map[string]int{}
	if err := eachRow(ctx, q, `
		SELECT COALESCE(NULLIF(LOWER(TRIM(COALESCE(NULLIF(p.method, ''), o.method))), ''), 'other'), `+paymentCur+`,
			COALESCE(SUM(`+paymentAmt+`), 0), COUNT(*) FILTER (WHERE p.event_type = 'charge')
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id
		LEFT JOIN payments o ON o.id = p.reverses_payment_id`+where+`
		  AND `+countedPayment+` AND `+inPeriodDays+`
		  AND (p.event_type = 'charge' OR (p.event_type = 'reverse' AND o.event_type = 'charge'))
		GROUP BY 1, 2`, args, func(r pgx.Rows) error {
		var method, cur string
		var amt int64
		var n int
		if err := r.Scan(&method, &cur, &amt, &n); err != nil {
			return err
		}
		i, ok := methods[method]
		if !ok {
			i = len(f.Methods)
			methods[method] = i
			f.Methods = append(f.Methods, dashboard.MethodAmounts{Method: method})
		}
		f.Methods[i].Amounts = append(f.Methods[i].Amounts, dashboard.Amount{Currency: cur, Minor: amt, Count: n})
		return nil
	}); err != nil {
		return nil, err
	}

	byDay := map[time.Time]int{}
	if err := eachRow(ctx, q, `
		SELECT p.received_at, `+paymentCur+`, COALESCE(SUM(`+paymentAmt+`), 0)
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id`+where+`
		  AND `+countedPayment+` AND `+inPeriodDays+`
		GROUP BY 1, 2
		ORDER BY 1`, args, func(r pgx.Rows) error {
		var day time.Time
		var cur string
		var amt int64
		if err := r.Scan(&day, &cur, &amt); err != nil {
			return err
		}
		i, ok := byDay[day]
		if !ok {
			i = len(f.Daily)
			byDay[day] = i
			f.Daily = append(f.Daily, dashboard.DayAmounts{Day: day})
		}
		f.Daily[i].Amounts = append(f.Daily[i].Amounts, dashboard.Amount{Currency: cur, Minor: amt})
		return nil
	}); err != nil {
		return nil, err
	}

	if where, args, err = bind(); err != nil {
		return nil, err
	}
	if f.Outstanding, err = amounts(ctx, q, `
		SELECT `+bookingCur+`, COALESCE(SUM(`+bookingAmt("b.balance_amt")+`), 0), COUNT(*)
		FROM bookings b`+where+`
		  AND b.status IN `+activeBookings+` AND b.balance_amt > 0
		GROUP BY 1`, args); err != nil {
		return nil, err
	}
	// Instalments have no FX snapshot, so they stay in their own currency.
	if where, args, err = bind(rq.Now, rq.Now.Add(dashboard.DueSoonWindow)); err != nil {
		return nil, err
	}
	overdue := `s.due_at < $3`
	dueSoon := `s.status = 'open' AND s.due_at >= $3`
	if err := eachRow(ctx, q, `
		SELECT COALESCE(NULLIF(s.currency, ''), $2),
			COALESCE(SUM(s.amount) FILTER (WHERE `+overdue+`), 0), COUNT(*) FILTER (WHERE `+overdue+`),
			COALESCE(SUM(s.amount) FILTER (WHERE `+dueSoon+`), 0), COUNT(*) FILTER (WHERE `+dueSoon+`)
		FROM payment_schedules s
		JOIN bookings b ON b.id = s.booking_id`+where+`
		  AND b.status <> 'cancelled'
		  AND s.status IN ('open','overdue') AND s.due_at < $4
		GROUP BY 1`, args, func(r pgx.Rows) error {
		var cur string
		var od, ds int64
		var nOd, nDs int
		if err := r.Scan(&cur, &od, &nOd, &ds, &nDs); err != nil {
			return err
		}
		f.Overdue = append(f.Overdue, dashboard.Amount{Currency: cur, Minor: od, Count: nOd})
		f.DueSoon = append(f.DueSoon, dashboard.Amount{Currency: cur, Minor: ds, Count: nDs})
		return nil
	}); err != nil {
		return nil, err
	}
	return f, nil
}

func amounts(ctx context.Context, q tx.Querier, sql string, args []any) ([]dashboard.Amount, error) {
	var out []dashboard.Amount
	err := eachRow(ctx, q, sql, args, func(r pgx.Rows) error {
		var a dashboard.Amount
		if err := r.Scan(&a.Currency, &a.Minor, &a.Count); err != nil {
			return err
		}
		out = append(out, a)
		return nil
	})
	return out, err
}

func eachRow(ctx context.Context, q tx.Querier, sql string, args []any, fn func(pgx.Rows) error) error {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func dayOf(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
