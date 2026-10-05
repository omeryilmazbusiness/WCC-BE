package finance

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
)

func (r *Repository) profitWhere(ctx context.Context, f app.ProfitFilter) (*where, error) {
	w := newWhere(f.BranchID, "b.branch_id").add("b.status IN "+revenueBookings).
		add("b.created_at >= ?", f.From).add("b.created_at < ?", f.To)
	return w, w.scope(ctx, ownedBooks)
}

func (r *Repository) BookingPnL(ctx context.Context, f app.ProfitFilter) ([]app.BookingPnLRow, error) {
	w, err := r.profitWhere(ctx, f)
	if err != nil {
		return nil, err
	}
	w.args = append(w.args, f.Limit)
	rows, err := r.q(ctx).Query(ctx, `
		SELECT b.id, b.ref_no, COALESCE(c.full_name,''), COALESCE(u.full_name,''), b.service_type, b.status,
			COALESCE(b.currency,'SAR'), b.total_amount, b.cost_amt, b.tax_amt, b.fee_amt, b.created_at
		FROM bookings b
		LEFT JOIN customers c ON c.id=b.customer_id
		LEFT JOIN users u ON u.id=b.owner_id
		WHERE `+w.sql()+`
		ORDER BY b.created_at DESC LIMIT `+placeholder(len(w.args)), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.BookingPnLRow{}
	for rows.Next() {
		var p app.BookingPnLRow
		if err := rows.Scan(&p.BookingID, &p.RefNo, &p.CustomerName, &p.OwnerName, &p.ServiceType, &p.Status, &p.Currency,
			&p.PnL.Gross, &p.PnL.Net, &p.PnL.Tax, &p.PnL.Fee, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.PnL.Costed = p.PnL.Net > 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeparturePnL covers departures that sold in the window; their actuals
// include every revenue booking of the departure, not only the window's.
func (r *Repository) DeparturePnL(ctx context.Context, f app.ProfitFilter) ([]app.DeparturePnLRow, error) {
	w, err := r.profitWhere(ctx, f)
	if err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `
		WITH deps AS (SELECT DISTINCT b.departure_id FROM bookings b WHERE `+w.sql()+`)
		SELECT d.id, COALESCE(p.name_en,''), d.depart_date, COALESCE(b.currency,'SAR'),
			COUNT(*)::int, COALESCE(SUM(b.pax_count),0)::int,
			COALESCE(SUM(b.total_amount),0)::bigint,
			COALESCE(SUM(b.cost_amt),0)::bigint,
			COALESCE(SUM(b.tax_amt),0)::bigint,
			COALESCE(SUM(b.fee_amt),0)::bigint,
			COUNT(*) FILTER (WHERE b.cost_amt > 0)::int,
			db.currency, db.revenue, db.cost, db.note
		FROM deps
		JOIN departures d ON d.id = deps.departure_id
		LEFT JOIN packages p ON p.id = d.package_id
		JOIN bookings b ON b.departure_id = d.id AND b.status IN `+revenueBookings+`
		LEFT JOIN departure_budgets db ON db.departure_id = d.id
		GROUP BY d.id, p.name_en, d.depart_date, COALESCE(b.currency,'SAR'), db.currency, db.revenue, db.cost, db.note
		ORDER BY d.depart_date, d.id
		LIMIT 100`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.DeparturePnLRow{}
	for rows.Next() {
		var d app.DeparturePnLRow
		var departs time.Time
		var costed int
		var bCur, bNote *string
		var bRev, bCost *int64
		if err := rows.Scan(&d.DepartureID, &d.PackageName, &departs, &d.Currency, &d.Bookings, &d.Pax,
			&d.PnL.Gross, &d.PnL.Net, &d.PnL.Tax, &d.PnL.Fee, &costed, &bCur, &bRev, &bCost, &bNote); err != nil {
			return nil, err
		}
		d.DepartsOn = &departs
		d.PnL.Costed = costed > 0
		if bCur != nil {
			d.Budget = &domain.Budget{DepartureID: d.DepartureID, Currency: *bCur, Revenue: *bRev, Cost: *bCost, Note: *bNote}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) RepPnL(ctx context.Context, f app.ProfitFilter) ([]app.RepPnLRow, error) {
	w, err := r.profitWhere(ctx, f)
	if err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `
		SELECT b.owner_id, COALESCE(u.full_name,''), COALESCE(b.currency,'SAR'),
			COALESCE(array_agg(b.total_amount - b.tax_amt - b.fee_amt - b.cost_amt) FILTER (WHERE b.cost_amt > 0), '{}')::bigint[],
			COALESCE(SUM(b.total_amount) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COALESCE(SUM(b.cost_amt) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COALESCE(SUM(b.tax_amt) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COALESCE(SUM(b.fee_amt) FILTER (WHERE b.cost_amt > 0),0)::bigint,
			COUNT(*)::int, COUNT(*) FILTER (WHERE b.cost_amt = 0)::int
		FROM bookings b LEFT JOIN users u ON u.id=b.owner_id
		WHERE `+w.sql()+`
		GROUP BY b.owner_id, u.full_name, COALESCE(b.currency,'SAR')
		ORDER BY 5 DESC`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.RepPnLRow{}
	for rows.Next() {
		var p app.RepPnLRow
		if err := rows.Scan(&p.UserID, &p.Name, &p.Currency, &p.Margins, &p.PnL.Gross, &p.PnL.Net, &p.PnL.Tax, &p.PnL.Fee,
			&p.Bookings, &p.Uncosted); err != nil {
			return nil, err
		}
		p.PnL.Costed = len(p.Margins) > 0
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) DepartureBranch(ctx context.Context, departureID uuid.UUID) (uuid.UUID, error) {
	clause, args, err := pgscope.Clause(ctx, branchOnly("p.branch_id"), []any{departureID})
	if err != nil {
		return uuid.Nil, err
	}
	var branch uuid.UUID
	err = r.q(ctx).QueryRow(ctx, `SELECT p.branch_id FROM departures d JOIN packages p ON p.id=d.package_id
		WHERE d.id=$1`+clause, args...).Scan(&branch)
	return branch, notFound(err, "departure")
}

func (r *Repository) UpsertBudget(ctx context.Context, branchID uuid.UUID, b domain.Budget, actor uuid.UUID) error {
	if err := pgscope.EnsureBranch(ctx, branchID); err != nil {
		return err
	}
	_, err := r.q(ctx).Exec(ctx, `
		INSERT INTO departure_budgets (departure_id, branch_id, currency, revenue, cost, note, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (departure_id) DO UPDATE SET currency=EXCLUDED.currency, revenue=EXCLUDED.revenue,
			cost=EXCLUDED.cost, note=EXCLUDED.note, updated_by=EXCLUDED.updated_by, updated_at=NOW()`,
		b.DepartureID, branchID, b.Currency, b.Revenue, b.Cost, b.Note, actor)
	return err
}

// ---- payables ----

func (r *Repository) DueInvoices(ctx context.Context, branchID *uuid.UUID, until time.Time) ([]app.DueInvoice, error) {
	w := newWhere(branchID, "i.branch_id").add("i.status IN ('submitted','approved')").add("i.grand_total > 0").
		add("(i.due_on IS NULL OR i.due_on <= ?)", until)
	if err := w.scope(ctx, branchOnly("i.branch_id")); err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `
		SELECT i.id, i.supplier_id, COALESCE(s.name_en,''), i.invoice_number, i.status, i.currency, i.grand_total, i.due_on
		FROM supplier_invoices i JOIN suppliers s ON s.id=i.supplier_id
		WHERE `+w.sql()+`
		ORDER BY i.due_on NULLS LAST, i.created_at LIMIT 300`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.DueInvoice{}
	for rows.Next() {
		var d app.DueInvoice
		if err := rows.Scan(&d.ID, &d.SupplierID, &d.SupplierName, &d.InvoiceNumber, &d.Status, &d.Currency, &d.Amount, &d.DueOn); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
