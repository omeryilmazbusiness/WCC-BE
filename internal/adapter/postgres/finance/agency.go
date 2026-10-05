package finance

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
)

const agencyColumns = `g.id, g.branch_id, g.code, g.name, g.contact_name, g.phone, g.email, g.tax_id, g.currency,
	g.credit_limit, g.payment_terms_days, g.grace_days, g.auto_suspend, g.status, g.suspend_reason, g.created_at, g.updated_at`

func scanAgency(row pgx.Row) (*domain.Agency, error) {
	var a domain.Agency
	err := row.Scan(&a.ID, &a.BranchID, &a.Code, &a.Name, &a.ContactName, &a.Phone, &a.Email, &a.TaxID, &a.Currency,
		&a.CreditLimit, &a.PaymentTermsDays, &a.GraceDays, &a.AutoSuspend, &a.Status, &a.SuspendReason, &a.CreatedAt, &a.UpdatedAt)
	return &a, err
}

func collectAgencies(rows pgx.Rows) ([]domain.Agency, error) {
	defer rows.Close()
	out := []domain.Agency{}
	for rows.Next() {
		a, err := scanAgency(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *Repository) ListAgencies(ctx context.Context, branchID *uuid.UUID) ([]domain.Agency, error) {
	w := newWhere(branchID, "g.branch_id")
	if err := w.scope(ctx, branchOnly("g.branch_id")); err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `SELECT `+agencyColumns+` FROM agencies g WHERE `+w.sql()+` ORDER BY g.name`, w.args...)
	if err != nil {
		return nil, err
	}
	return collectAgencies(rows)
}

func (r *Repository) ListActiveForSweep(ctx context.Context) ([]domain.Agency, error) {
	rows, err := r.q(ctx).Query(ctx, `SELECT `+agencyColumns+` FROM agencies g WHERE g.status='active' AND g.auto_suspend`)
	if err != nil {
		return nil, err
	}
	return collectAgencies(rows)
}

func (r *Repository) findAgency(ctx context.Context, id uuid.UUID, lock string) (*domain.Agency, error) {
	clause, args, err := pgscope.Clause(ctx, branchOnly("g.branch_id"), []any{id})
	if err != nil {
		return nil, err
	}
	a, err := scanAgency(r.q(ctx).QueryRow(ctx, `SELECT `+agencyColumns+` FROM agencies g WHERE g.id=$1`+clause+lock, args...))
	if err != nil {
		return nil, notFound(err, "agency")
	}
	return a, nil
}

func (r *Repository) FindAgency(ctx context.Context, id uuid.UUID) (*domain.Agency, error) {
	return r.findAgency(ctx, id, "")
}

func (r *Repository) LockAgency(ctx context.Context, id uuid.UUID) (*domain.Agency, error) {
	return r.findAgency(ctx, id, " FOR UPDATE")
}

func (r *Repository) InsertAgency(ctx context.Context, a *domain.Agency) error {
	if err := pgscope.EnsureBranch(ctx, a.BranchID); err != nil {
		return err
	}
	_, err := r.q(ctx).Exec(ctx, `INSERT INTO agencies (id, branch_id, code, name, contact_name, phone, email, tax_id,
		currency, credit_limit, payment_terms_days, grace_days, auto_suspend, status, suspend_reason, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		a.ID, a.BranchID, a.Code, a.Name, a.ContactName, a.Phone, a.Email, a.TaxID, a.Currency, a.CreditLimit,
		a.PaymentTermsDays, a.GraceDays, a.AutoSuspend, a.Status, a.SuspendReason, a.CreatedAt, a.UpdatedAt)
	return err
}

func (r *Repository) UpdateAgency(ctx context.Context, a *domain.Agency) error {
	_, err := r.q(ctx).Exec(ctx, `UPDATE agencies SET code=$2, name=$3, contact_name=$4, phone=$5, email=$6, tax_id=$7,
		currency=$8, credit_limit=$9, payment_terms_days=$10, grace_days=$11, auto_suspend=$12, status=$13,
		suspend_reason=$14, updated_at=$15 WHERE id=$1`,
		a.ID, a.Code, a.Name, a.ContactName, a.Phone, a.Email, a.TaxID, a.Currency, a.CreditLimit, a.PaymentTermsDays,
		a.GraceDays, a.AutoSuspend, a.Status, a.SuspendReason, a.UpdatedAt)
	return err
}

func (r *Repository) SetBookingAgency(ctx context.Context, bookingID uuid.UUID, agencyID *uuid.UUID) error {
	clause, args, err := pgscope.Clause(ctx, branchOnly("branch_id"), []any{bookingID, agencyID})
	if err != nil {
		return err
	}
	ct, err := r.q(ctx).Exec(ctx, `UPDATE bookings SET agency_id=$2,
		channel=CASE WHEN $2::uuid IS NOT NULL THEN 'b2b_agency' ELSE channel END, updated_at=NOW()
		WHERE id=$1`+clause, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return notFound(pgx.ErrNoRows, "booking")
	}
	return nil
}

// Exposures computes outstanding and overdue debt per agency in its own
// currency. Bookings are counted branch-wide: an agency's debt does not
// depend on which employee owns the booking.
func (r *Repository) Exposures(ctx context.Context, branchID *uuid.UUID, today time.Time) (map[uuid.UUID]domain.Exposure, error) {
	w := newWhere(branchID, "b.branch_id").add("b.status IN " + activeBookings).add("b.balance_amt > 0").add("b.agency_id IS NOT NULL")
	if err := w.scope(ctx, branchOnly("b.branch_id")); err != nil {
		return nil, err
	}
	w.args = append(w.args, today)
	t := placeholder(len(w.args))
	rows, err := r.q(ctx).Query(ctx, dueCTE(w.sql())+`,
		agg AS (
			SELECT d.agency_id, d.currency,
				SUM(d.amt) FILTER (WHERE d.due_on < `+t+`::date) AS overdue,
				MIN(d.due_on) FILTER (WHERE d.due_on < `+t+`::date) AS oldest
			FROM dues d GROUP BY 1, 2
		), outstanding AS (
			SELECT o.agency_id, o.currency, SUM(o.balance_amt) AS total, COUNT(*) AS n FROM open_b o GROUP BY 1, 2
		)
		SELECT g.id, COALESCE(os.total, 0)::bigint, COALESCE(ag.overdue, 0)::bigint,
			COALESCE(`+t+`::date - ag.oldest, 0)::int, COALESCE(os.n, 0)::int,
			EXISTS (SELECT 1 FROM outstanding x WHERE x.agency_id = g.id AND x.currency <> g.currency)
		FROM agencies g
		LEFT JOIN outstanding os ON os.agency_id = g.id AND os.currency = g.currency
		LEFT JOIN agg ag ON ag.agency_id = g.id AND ag.currency = g.currency
		WHERE ($1::uuid IS NULL OR g.branch_id = $1)`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]domain.Exposure{}
	for rows.Next() {
		var id uuid.UUID
		var e domain.Exposure
		if err := rows.Scan(&id, &e.Outstanding, &e.Overdue, &e.OldestOverdueDays, &e.OpenBookings, &e.UnconvertedBalance); err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, rows.Err()
}
