package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Insert(ctx context.Context, p *domain.Payment) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO payments (id, booking_id, amount, currency, method, reference, recorded_by, idempotency_key, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		p.ID, p.BookingID, p.Amount, p.Currency, p.Method, p.Reference, p.RecordedBy, p.IdempotencyKey, p.CreatedAt,
	)
	return err
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, booking_id, amount, currency, method, reference, recorded_by, idempotency_key, created_at
		FROM payments WHERE idempotency_key=$1`, key)
	var p domain.Payment
	err := row.Scan(&p.ID, &p.BookingID, &p.Amount, &p.Currency, &p.Method, &p.Reference, &p.RecordedBy, &p.IdempotencyKey, &p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return &p, err
}

func (r *Repository) SumByBooking(ctx context.Context, bookingID uuid.UUID) (int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var sum int64
	err := q.QueryRow(ctx, `SELECT COALESCE(SUM(amount),0) FROM payments WHERE booking_id=$1`, bookingID).Scan(&sum)
	return sum, err
}

func (r *Repository) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, booking_id, amount, currency, method, reference, recorded_by, idempotency_key, created_at
		FROM payments WHERE booking_id=$1 ORDER BY created_at`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Payment
	for rows.Next() {
		var p domain.Payment
		if err := rows.Scan(&p.ID, &p.BookingID, &p.Amount, &p.Currency, &p.Method, &p.Reference, &p.RecordedBy, &p.IdempotencyKey, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
