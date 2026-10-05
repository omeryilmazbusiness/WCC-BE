package supplier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func (r *Repository) ListSummaries(ctx context.Context, f domain.ListFilter) ([]domain.Summary, error) {
	args := []any{f.BranchID, f.Since}
	where := []string{"($1::uuid IS NULL OR s.branch_id=$1)"}
	if f.ActiveOnly {
		where = append(where, "s.is_active")
	}
	if f.Category != "" {
		args = append(args, f.Category)
		where = append(where, fmt.Sprintf("s.category=$%d", len(args)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf(`(s.code ILIKE $%[1]d OR s.name_en ILIKE $%[1]d OR s.name_ar ILIKE $%[1]d
			OR s.contact_name ILIKE $%[1]d OR s.contact_email ILIKE $%[1]d)`, n))
	}
	clause, args, err := pgscope.Clause(ctx, parentScope, args)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT `+supplierColumns+`,
			s.credentials_enc <> '',
			(SELECT COUNT(*) FROM supplier_disputes d WHERE d.supplier_id=s.id AND d.status='open'),
			COALESCE((SELECT SUM(e.amount) FROM supplier_ledger_entries e
				WHERE e.supplier_id=s.id AND e.kind='charge' AND e.created_at >= $2), 0),
			(SELECT COUNT(*) FROM supplier_ledger_entries e
				WHERE e.supplier_id=s.id AND e.kind='charge' AND e.created_at >= $2)
		FROM suppliers s
		WHERE `+strings.Join(where, " AND ")+clause+`
		ORDER BY s.is_active DESC, s.code`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Summary{}
	for rows.Next() {
		var sum domain.Summary
		s, err := scanSupplier(rows, &sum.HasCredentials, &sum.OpenDisputes, &sum.Spend, &sum.Bookings)
		if err != nil {
			return nil, err
		}
		sum.Supplier = *s
		out = append(out, sum)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (r *Repository) LoadCredentials(ctx context.Context, id uuid.UUID) (string, error) {
	clause, args, err := pgscope.Clause(ctx, parentScope, []any{id})
	if err != nil {
		return "", err
	}
	var sealed string
	err = tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT s.credentials_enc FROM suppliers s WHERE s.id=$1`+clause, args...).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", shared.NewNotFound("supplier")
	}
	return sealed, err
}

func (r *Repository) StoreCredentials(ctx context.Context, id uuid.UUID, sealed string) error {
	clause, args, err := pgscope.Clause(ctx, supplierScope, []any{id, sealed})
	if err != nil {
		return err
	}
	ct, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx,
		`UPDATE suppliers SET credentials_enc=$2, updated_at=NOW() WHERE id=$1`+clause, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return shared.NewNotFound("supplier")
	}
	return nil
}

func (r *Repository) ListContractsEnding(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]domain.Supplier, error) {
	clause, args, err := pgscope.Clause(ctx, parentScope, []any{branchID, from, to})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `SELECT `+supplierColumns+` FROM suppliers s
		WHERE ($1::uuid IS NULL OR s.branch_id=$1) AND s.is_active
			AND s.contract_end BETWEEN $2::date AND $3::date`+clause+`
		ORDER BY s.contract_end`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Supplier
	for rows.Next() {
		s, err := scanSupplier(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *Repository) CreateLedgerEntry(ctx context.Context, e *domain.LedgerEntry) error {
	if err := r.ensureSupplier(ctx, e.SupplierID, &e.BranchID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO supplier_ledger_entries (
			id, supplier_id, branch_id, kind, amount, currency, balance_after, reference, note, actor_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ID, e.SupplierID, e.BranchID, e.Kind, e.Amount, e.Currency, e.BalanceAfter, e.Reference, e.Note,
		e.ActorID, e.CreatedAt)
	return err
}

func (r *Repository) ListLedger(ctx context.Context, supplierID uuid.UUID, limit int) ([]domain.LedgerEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	guard, args, err := supplierVisible(ctx, "e.supplier_id", []any{supplierID, limit})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT e.id, e.supplier_id, e.branch_id, e.kind, e.amount, e.currency, e.balance_after, e.reference,
			e.note, e.actor_id, e.created_at
		FROM supplier_ledger_entries e WHERE e.supplier_id=$1`+guard+`
		ORDER BY e.created_at DESC, e.id DESC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.LedgerEntry{}
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.SupplierID, &e.BranchID, &e.Kind, &e.Amount, &e.Currency, &e.BalanceAfter,
			&e.Reference, &e.Note, &e.ActorID, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) VolumeSince(ctx context.Context, supplierID uuid.UUID, since time.Time) (domain.Volume, error) {
	guard, args, err := supplierVisible(ctx, "e.supplier_id", []any{supplierID, since})
	if err != nil {
		return domain.Volume{}, err
	}
	var v domain.Volume
	err = tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT COALESCE(SUM(e.amount) FILTER (WHERE e.kind='charge'), 0),
			COUNT(*) FILTER (WHERE e.kind='charge'),
			COALESCE(SUM(e.amount) FILTER (WHERE e.kind='refund'), 0)
		FROM supplier_ledger_entries e WHERE e.supplier_id=$1 AND e.created_at >= $2`+guard, args...,
	).Scan(&v.Spend, &v.Bookings, &v.Refunds)
	return v, err
}

func (r *Repository) AddUsage(ctx context.Context, u *domain.Usage) error {
	if err := r.ensureSupplier(ctx, u.SupplierID, nil); err != nil {
		return err
	}
	samples := 0
	if u.LatencyMs > 0 {
		samples = 1
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO supplier_api_stats AS t (
			supplier_id, day, searches, bookings, errors, price_changes, sold_outs, latency_ms_total, latency_samples
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (supplier_id, day) DO UPDATE SET
			searches = t.searches + EXCLUDED.searches,
			bookings = t.bookings + EXCLUDED.bookings,
			errors = t.errors + EXCLUDED.errors,
			price_changes = t.price_changes + EXCLUDED.price_changes,
			sold_outs = t.sold_outs + EXCLUDED.sold_outs,
			latency_ms_total = t.latency_ms_total + EXCLUDED.latency_ms_total,
			latency_samples = t.latency_samples + EXCLUDED.latency_samples`,
		u.SupplierID, domain.FormatDay(u.Day), u.Searches, u.Bookings, u.Errors, u.PriceChanges, u.SoldOuts,
		u.LatencyMs, samples)
	return err
}

func (r *Repository) MetricsSince(ctx context.Context, supplierID uuid.UUID, since time.Time) (domain.Metrics, error) {
	guard, args, err := supplierVisible(ctx, "t.supplier_id", []any{supplierID, domain.FormatDay(since)})
	if err != nil {
		return domain.Metrics{}, err
	}
	var m domain.Metrics
	var latTotal, latSamples int64
	err = tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		SELECT COALESCE(SUM(t.searches),0), COALESCE(SUM(t.bookings),0), COALESCE(SUM(t.errors),0),
			COALESCE(SUM(t.price_changes),0), COALESCE(SUM(t.sold_outs),0),
			COALESCE(SUM(t.latency_ms_total),0), COALESCE(SUM(t.latency_samples),0)
		FROM supplier_api_stats t WHERE t.supplier_id=$1 AND t.day >= $2::date`+guard, args...,
	).Scan(&m.Searches, &m.Bookings, &m.Errors, &m.PriceChanges, &m.SoldOuts, &latTotal, &latSamples)
	if latSamples > 0 {
		m.AvgLatencyMs = int(latTotal / latSamples)
	}
	return m, err
}

const disputeColumns = `d.id, d.supplier_id, d.branch_id, d.title, d.booking_ref, d.amount, d.currency, d.status,
	d.resolution, d.opened_by, d.opened_at, d.resolved_at, d.updated_at`

func scanDispute(row pgx.Row) (*domain.Dispute, error) {
	var d domain.Dispute
	err := row.Scan(&d.ID, &d.SupplierID, &d.BranchID, &d.Title, &d.BookingRef, &d.Amount, &d.Currency, &d.Status,
		&d.Resolution, &d.OpenedBy, &d.OpenedAt, &d.ResolvedAt, &d.UpdatedAt)
	return &d, err
}

func (r *Repository) CreateDispute(ctx context.Context, d *domain.Dispute) error {
	if err := r.ensureSupplier(ctx, d.SupplierID, &d.BranchID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO supplier_disputes (
			id, supplier_id, branch_id, title, booking_ref, amount, currency, status, resolution, opened_by,
			opened_at, resolved_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		d.ID, d.SupplierID, d.BranchID, d.Title, d.BookingRef, d.Amount, d.Currency, d.Status, d.Resolution,
		d.OpenedBy, d.OpenedAt, d.ResolvedAt, d.UpdatedAt)
	return err
}

func (r *Repository) UpdateDispute(ctx context.Context, d *domain.Dispute) error {
	guard, args, err := supplierVisible(ctx, "d.supplier_id", []any{d.ID, d.Status, d.Resolution, d.ResolvedAt, d.UpdatedAt})
	if err != nil {
		return err
	}
	ct, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE supplier_disputes d SET status=$2, resolution=$3, resolved_at=$4, updated_at=$5
		WHERE d.id=$1`+guard, args...)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return shared.NewNotFound("dispute")
	}
	return nil
}

func (r *Repository) FindDispute(ctx context.Context, id uuid.UUID) (*domain.Dispute, error) {
	guard, args, err := supplierVisible(ctx, "d.supplier_id", []any{id})
	if err != nil {
		return nil, err
	}
	d, err := scanDispute(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT `+disputeColumns+` FROM supplier_disputes d WHERE d.id=$1`+guard+` FOR UPDATE OF d`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("dispute")
	}
	return d, err
}

func (r *Repository) ListDisputes(ctx context.Context, supplierID uuid.UUID) ([]domain.Dispute, error) {
	guard, args, err := supplierVisible(ctx, "d.supplier_id", []any{supplierID})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `SELECT `+disputeColumns+` FROM supplier_disputes d
		WHERE d.supplier_id=$1`+guard+` ORDER BY (d.status='open') DESC, d.opened_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Dispute{}
	for rows.Next() {
		d, err := scanDispute(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}
