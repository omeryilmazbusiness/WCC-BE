package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var _ domain.PromiseRepository = (*Repository)(nil)

const promiseCols = `pp.id, pp.booking_id, pp.branch_id, pp.amount, pp.currency, pp.promised_on, pp.note, pp.status,
	pp.task_id, pp.created_by, pp.created_at, pp.resolved_at`

func scanPromise(scan func(dest ...any) error) (*domain.Promise, error) {
	var p domain.Promise
	var st string
	if err := scan(&p.ID, &p.BookingID, &p.BranchID, &p.Amount, &p.Currency, &p.PromisedOn, &p.Note, &st,
		&p.TaskID, &p.CreatedBy, &p.CreatedAt, &p.ResolvedAt); err != nil {
		return nil, err
	}
	p.Status = domain.PromiseStatus(st)
	return &p, nil
}

func (r *Repository) promiseRows(ctx context.Context, sql string, args ...any) ([]domain.Promise, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Promise{}
	for rows.Next() {
		p, err := scanPromise(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *Repository) InsertPromise(ctx context.Context, p *domain.Promise) error {
	if err := r.requireBooking(ctx, p.BookingID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO payment_promises (id, booking_id, branch_id, amount, currency, promised_on, note, status,
			task_id, created_by, created_at, resolved_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		p.ID, p.BookingID, p.BranchID, p.Amount, p.Currency, p.PromisedOn, p.Note, string(p.Status),
		p.TaskID, p.CreatedBy, p.CreatedAt, p.ResolvedAt)
	return err
}

func (r *Repository) GetPromise(ctx context.Context, id uuid.UUID) (*domain.Promise, error) {
	scope, args, err := bookingVisible(ctx, "pp.booking_id", []any{id})
	if err != nil {
		return nil, err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	p, err := scanPromise(q.QueryRow(ctx, `SELECT `+promiseCols+` FROM payment_promises pp WHERE pp.id=$1`+scope, args...).Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("payment promise")
	}
	return p, err
}

func (r *Repository) ListPromises(ctx context.Context, bookingID uuid.UUID) ([]domain.Promise, error) {
	scope, args, err := bookingVisible(ctx, "pp.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	return r.promiseRows(ctx, `SELECT `+promiseCols+` FROM payment_promises pp
		WHERE pp.booking_id=$1`+scope+` ORDER BY pp.promised_on, pp.created_at`, args...)
}

func (r *Repository) ListOpenPromises(ctx context.Context, afterID uuid.UUID, limit int) ([]domain.Promise, error) {
	if limit <= 0 {
		limit = 100
	}
	scope, args, err := bookingVisible(ctx, "pp.booking_id", []any{afterID})
	if err != nil {
		return nil, err
	}
	args = append(args, limit)
	return r.promiseRows(ctx, fmt.Sprintf(`SELECT `+promiseCols+` FROM payment_promises pp
		WHERE pp.status='open' AND pp.id > $1`+scope+` ORDER BY pp.id LIMIT $%d`, len(args)), args...)
}

func (r *Repository) ResolvePromise(ctx context.Context, p *domain.Promise) (bool, error) {
	scope, args, err := bookingVisible(ctx, "payment_promises.booking_id", []any{p.ID, string(p.Status), p.ResolvedAt})
	if err != nil {
		return false, err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		UPDATE payment_promises SET status=$2, resolved_at=$3
		WHERE id=$1 AND status='open'`+scope, args...)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) SumCollectedSince(ctx context.Context, bookingID uuid.UUID, currency string, since time.Time) (int64, error) {
	scope, args, err := bookingVisible(ctx, "p.booking_id", []any{bookingID, currency, since})
	if err != nil {
		return 0, err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var sum int64
	err = q.QueryRow(ctx, `
		SELECT COALESCE(SUM(p.amount),0) FROM payments p
		WHERE p.booking_id=$1 AND p.currency=$2 AND p.created_at >= $3
		  AND p.status IN ('verified','approved')`+scope, args...).Scan(&sum)
	return sum, err
}
