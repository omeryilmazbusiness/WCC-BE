package finance

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
)

const accountColumns = `a.id, a.branch_id, a.kind, a.name, a.currency, a.bank_name, a.iban, a.commission_bps,
	a.balance, a.low_balance_threshold, a.is_active, a.created_at, a.updated_at`

func scanAccount(row pgx.Row) (*domain.Account, error) {
	var a domain.Account
	err := row.Scan(&a.ID, &a.BranchID, &a.Kind, &a.Name, &a.Currency, &a.BankName, &a.IBAN, &a.CommissionBPS,
		&a.Balance, &a.LowBalanceThreshold, &a.IsActive, &a.CreatedAt, &a.UpdatedAt)
	return &a, err
}

func (r *Repository) ListAccounts(ctx context.Context, branchID *uuid.UUID) ([]domain.Account, error) {
	w := newWhere(branchID, "a.branch_id")
	if err := w.scope(ctx, branchOnly("a.branch_id")); err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `SELECT `+accountColumns+` FROM treasury_accounts a WHERE `+w.sql()+`
		ORDER BY a.is_active DESC, a.kind, a.currency, a.name`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *Repository) findAccount(ctx context.Context, id uuid.UUID, lock string) (*domain.Account, error) {
	clause, args, err := pgscope.Clause(ctx, branchOnly("a.branch_id"), []any{id})
	if err != nil {
		return nil, err
	}
	a, err := scanAccount(r.q(ctx).QueryRow(ctx, `SELECT `+accountColumns+` FROM treasury_accounts a WHERE a.id=$1`+clause+lock, args...))
	if err != nil {
		return nil, notFound(err, "treasury account")
	}
	return a, nil
}

func (r *Repository) FindAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	return r.findAccount(ctx, id, "")
}

func (r *Repository) LockAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	return r.findAccount(ctx, id, " FOR UPDATE")
}

func (r *Repository) InsertAccount(ctx context.Context, a *domain.Account) error {
	if err := pgscope.EnsureBranch(ctx, a.BranchID); err != nil {
		return err
	}
	_, err := r.q(ctx).Exec(ctx, `INSERT INTO treasury_accounts (id, branch_id, kind, name, currency, bank_name, iban,
		commission_bps, balance, low_balance_threshold, is_active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		a.ID, a.BranchID, a.Kind, a.Name, a.Currency, a.BankName, a.IBAN, a.CommissionBPS, a.Balance,
		a.LowBalanceThreshold, a.IsActive, a.CreatedAt, a.UpdatedAt)
	return err
}

func (r *Repository) UpdateAccount(ctx context.Context, a *domain.Account) error {
	_, err := r.q(ctx).Exec(ctx, `UPDATE treasury_accounts SET name=$2, bank_name=$3, iban=$4, commission_bps=$5,
		low_balance_threshold=$6, is_active=$7, updated_at=$8 WHERE id=$1`,
		a.ID, a.Name, a.BankName, a.IBAN, a.CommissionBPS, a.LowBalanceThreshold, a.IsActive, a.UpdatedAt)
	return err
}

func (r *Repository) SetBalance(ctx context.Context, id uuid.UUID, balance int64) error {
	_, err := r.q(ctx).Exec(ctx, `UPDATE treasury_accounts SET balance=$2, updated_at=NOW() WHERE id=$1`, id, balance)
	return err
}

func (r *Repository) InsertMovement(ctx context.Context, m *domain.Movement) (bool, error) {
	ct, err := r.q(ctx).Exec(ctx, `INSERT INTO treasury_movements (id, account_id, branch_id, direction, kind, amount, fee,
		currency, balance_after, booking_id, supplier_id, counter_account_id, transfer_id, source, external_id, reference,
		counterparty, note, match_status, matched_payment_id, occurred_on, actor_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		ON CONFLICT (account_id, external_id) WHERE external_id <> '' DO NOTHING`,
		m.ID, m.AccountID, m.BranchID, m.Direction, m.Kind, m.Amount, m.Fee, m.Currency, m.BalanceAfter, m.BookingID,
		m.SupplierID, m.CounterAccountID, m.TransferID, m.Source, m.ExternalID, m.Reference, m.Counterparty, m.Note,
		m.MatchStatus, m.MatchedPaymentID, m.OccurredOn, m.ActorID, m.CreatedAt)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

const movementColumns = `m.id, m.account_id, m.branch_id, m.direction, m.kind, m.amount, m.fee, m.currency, m.balance_after,
	m.booking_id, m.supplier_id, m.counter_account_id, m.transfer_id, m.source, m.external_id, m.reference,
	m.counterparty, m.note, m.match_status, m.matched_payment_id, m.occurred_on, m.actor_id, m.created_at`

func scanMovement(row pgx.Row, extra ...any) (*domain.Movement, error) {
	var m domain.Movement
	dst := []any{&m.ID, &m.AccountID, &m.BranchID, &m.Direction, &m.Kind, &m.Amount, &m.Fee, &m.Currency, &m.BalanceAfter,
		&m.BookingID, &m.SupplierID, &m.CounterAccountID, &m.TransferID, &m.Source, &m.ExternalID, &m.Reference,
		&m.Counterparty, &m.Note, &m.MatchStatus, &m.MatchedPaymentID, &m.OccurredOn, &m.ActorID, &m.CreatedAt}
	err := row.Scan(append(dst, extra...)...)
	return &m, err
}

func (r *Repository) FindMovement(ctx context.Context, id uuid.UUID) (*domain.Movement, error) {
	clause, args, err := pgscope.Clause(ctx, branchOnly("m.branch_id"), []any{id})
	if err != nil {
		return nil, err
	}
	m, err := scanMovement(r.q(ctx).QueryRow(ctx, `SELECT `+movementColumns+` FROM treasury_movements m WHERE m.id=$1`+clause, args...))
	if err != nil {
		return nil, notFound(err, "treasury movement")
	}
	return m, nil
}

func (r *Repository) ListMovements(ctx context.Context, f app.MovementFilter) ([]domain.Movement, error) {
	w := newWhere(f.BranchID, "m.branch_id")
	if f.AccountID != nil {
		w.add("m.account_id=?", *f.AccountID)
	}
	if f.UnmatchedOnly {
		w.add("m.match_status='unmatched'")
	}
	if err := w.scope(ctx, branchOnly("m.branch_id")); err != nil {
		return nil, err
	}
	w.args = append(w.args, f.Limit)
	rows, err := r.q(ctx).Query(ctx, `SELECT `+movementColumns+` FROM treasury_movements m WHERE `+w.sql()+`
		ORDER BY m.occurred_on DESC, m.created_at DESC LIMIT `+placeholder(len(w.args)), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Movement{}
	for rows.Next() {
		m, err := scanMovement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *Repository) ResolveMatch(ctx context.Context, id uuid.UUID, status string, bookingID, paymentID *uuid.UUID) error {
	ct, err := r.q(ctx).Exec(ctx, `UPDATE treasury_movements SET match_status=$2, booking_id=COALESCE($3, booking_id),
		matched_payment_id=$4 WHERE id=$1 AND match_status='unmatched'`, id, status, bookingID, paymentID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return conflict("this movement was already resolved")
	}
	return nil
}

func (r *Repository) POSStats(ctx context.Context, branchID *uuid.UUID, since time.Time) ([]app.POSStat, error) {
	w := newWhere(branchID, "a.branch_id").add("a.kind='pos'")
	if err := w.scope(ctx, branchOnly("a.branch_id")); err != nil {
		return nil, err
	}
	w.args = append(w.args, since)
	rows, err := r.q(ctx).Query(ctx, `
		SELECT a.id, a.name, a.currency, a.commission_bps,
			COALESCE(SUM(m.amount),0)::bigint, COALESCE(SUM(m.fee),0)::bigint, COUNT(m.id)::int
		FROM treasury_accounts a
		LEFT JOIN treasury_movements m ON m.account_id=a.id AND m.direction='in' AND m.kind='collection'
			AND m.occurred_on >= `+placeholder(len(w.args))+`::date
		WHERE `+w.sql()+`
		GROUP BY a.id ORDER BY 5 DESC`, w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []app.POSStat{}
	for rows.Next() {
		var s app.POSStat
		if err := rows.Scan(&s.AccountID, &s.Name, &s.Currency, &s.CommissionBPS, &s.Gross, &s.Fees, &s.Count); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---- bookings ----

func (r *Repository) BookingsByRefs(ctx context.Context, branchID uuid.UUID, refNos []int64, pnrs []string) ([]domain.BookingDue, error) {
	if refNos == nil {
		refNos = []int64{}
	}
	if pnrs == nil {
		pnrs = []string{}
	}
	clause, args, err := pgscope.Clause(ctx, branchOnly("b.branch_id"), []any{branchID, refNos, pnrs})
	if err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `
		SELECT b.id, b.ref_no, b.pnr, COALESCE(b.currency,'SAR'), b.balance_amt, COALESCE(c.full_name,'')
		FROM bookings b LEFT JOIN customers c ON c.id=b.customer_id
		WHERE b.branch_id=$1 AND b.status IN `+activeBookings+` AND b.balance_amt > 0
			AND (b.ref_no = ANY($2::bigint[]) OR (b.pnr <> '' AND upper(b.pnr) = ANY($3::text[])))`+clause+`
		LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.BookingDue
	for rows.Next() {
		var d domain.BookingDue
		if err := rows.Scan(&d.BookingID, &d.RefNo, &d.PNR, &d.Currency, &d.Balance, &d.CustomerName); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *Repository) BookingDue(ctx context.Context, id uuid.UUID) (*app.BookingFacts, error) {
	clause, args, err := pgscope.Clause(ctx, ownedBooks, []any{id})
	if err != nil {
		return nil, err
	}
	var b app.BookingFacts
	err = r.q(ctx).QueryRow(ctx, `
		SELECT b.id, b.branch_id, b.ref_no, b.pnr, b.status, COALESCE(b.currency,'SAR'), b.total_amount, b.collected_amt,
			b.balance_amt, b.cost_amt, b.tax_amt, b.fee_amt, COALESCE(c.full_name,''), COALESCE(c.phone,''),
			COALESCE(c.email,''), b.agency_id, b.service_type
		FROM bookings b LEFT JOIN customers c ON c.id=b.customer_id
		WHERE b.id=$1`+clause, args...).Scan(&b.ID, &b.BranchID, &b.RefNo, &b.PNR, &b.Status, &b.Currency, &b.Total,
		&b.Collected, &b.Balance, &b.Cost, &b.Tax, &b.Fee, &b.CustomerName, &b.CustomerPhone, &b.CustomerEmail,
		&b.AgencyID, &b.ServiceType)
	if err != nil {
		return nil, notFound(err, "booking")
	}
	return &b, nil
}
