// Package finance is the Postgres adapter of the finance hub.
package finance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Repository implements every finance hub port on one pool.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

var (
	_ app.SettingsStore     = (*Repository)(nil)
	_ app.OverviewReader    = (*Repository)(nil)
	_ app.TreasuryStore     = (*Repository)(nil)
	_ app.BookingLookup     = (*Repository)(nil)
	_ app.AgencyStore       = (*Repository)(nil)
	_ app.ReceivablesReader = (*Repository)(nil)
	_ app.PayablesReader    = (*Repository)(nil)
	_ app.ProfitReader      = (*Repository)(nil)
	_ app.BudgetStore       = (*Repository)(nil)
	_ app.BSPStore          = (*Repository)(nil)
	_ app.LetterStore       = (*Repository)(nil)
)

// Scope columns per table alias.
var (
	branchOnly = func(col string) pgscope.Columns { return pgscope.Columns{Branch: col} }
	ownedBooks = pgscope.Columns{Branch: "b.branch_id", Owner: "b.owner_id"}
)

const (
	activeBookings  = `('confirmed','partially_paid','ready','travelled')`
	revenueBookings = `('confirmed','partially_paid','ready','travelled','completed')`
	defaultCurrency = "SAR"
)

func (r *Repository) q(ctx context.Context) tx.Querier { return tx.QuerierFrom(ctx, r.pool) }

// where builds "branch filter AND scope" for one statement. The optional
// branch is always $1 of args.
type where struct {
	parts []string
	args  []any
}

func newWhere(branchID *uuid.UUID, branchCol string) *where {
	return &where{parts: []string{fmt.Sprintf("($1::uuid IS NULL OR %s=$1)", branchCol)}, args: []any{branchID}}
}

func (w *where) add(cond string, vals ...any) *where {
	for _, v := range vals {
		w.args = append(w.args, v)
		cond = strings.Replace(cond, "?", fmt.Sprintf("$%d", len(w.args)), 1)
	}
	w.parts = append(w.parts, cond)
	return w
}

func (w *where) scope(ctx context.Context, cols pgscope.Columns) error {
	var err error
	w.parts, w.args, err = pgscope.Append(ctx, cols, w.parts, w.args)
	return err
}

func (w *where) sql() string { return strings.Join(w.parts, " AND ") }

func placeholder(n int) string { return fmt.Sprintf("$%d", n) }

func sprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }

func replaceFirst(s, old, repl string) string { return strings.Replace(s, old, repl, 1) }

func conflict(msg string) error { return shared.NewConflict(msg) }

func notFound(err error, entity string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewNotFound(entity)
	}
	return err
}

// ---- settings ----

func (r *Repository) FinanceSettings(ctx context.Context, branchID *uuid.UUID) (app.Settings, error) {
	s := app.Settings{ReportingCurrency: defaultCurrency, CommissionBPS: domain.DefaultCommissionBPS}
	var err error
	if branchID != nil {
		err = r.q(ctx).QueryRow(ctx, `SELECT reporting_currency, commission_bps FROM finance_settings WHERE branch_id=$1`,
			*branchID).Scan(&s.ReportingCurrency, &s.CommissionBPS)
	} else {
		err = r.q(ctx).QueryRow(ctx, `SELECT reporting_currency FROM finance_settings
			GROUP BY reporting_currency ORDER BY COUNT(*) DESC, reporting_currency LIMIT 1`).Scan(&s.ReportingCurrency)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	return s, err
}

func (r *Repository) SaveFinanceRates(ctx context.Context, branchID uuid.UUID, commissionBPS int) error {
	if err := pgscope.EnsureBranch(ctx, branchID); err != nil {
		return err
	}
	_, err := r.q(ctx).Exec(ctx, `
		INSERT INTO finance_settings (branch_id, commission_bps, updated_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (branch_id) DO UPDATE SET commission_bps=EXCLUDED.commission_bps, updated_at=NOW()`, branchID, commissionBPS)
	return err
}

func (r *Repository) BranchName(ctx context.Context, branchID uuid.UUID) (string, error) {
	var name string
	err := r.q(ctx).QueryRow(ctx, `
		SELECT COALESCE(NULLIF(c.legal_name, ''), c.name_en) FROM branches b
		JOIN companies c ON c.id = b.company_id WHERE b.id=$1`, branchID).Scan(&name)
	return name, notFound(err, "branch")
}

func collectMoney(rows pgx.Rows) ([]app.Money, error) {
	defer rows.Close()
	out := []app.Money{}
	for rows.Next() {
		var m app.Money
		if err := rows.Scan(&m.Currency, &m.Amount, &m.Count); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
