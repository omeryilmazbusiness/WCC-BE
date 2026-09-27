package outbox

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/outbox"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Repository stores durable events in outbox_events.
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, name, payload, branch_id, status, attempts, next_attempt_at, last_error, created_at, dispatched_at`

// Insert joins the caller's transaction when ctx carries one.
func (r *Repository) Insert(ctx context.Context, name string, payload []byte, branchID *uuid.UUID) error {
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx,
		`INSERT INTO outbox_events (name, payload, branch_id) VALUES ($1, $2::jsonb, $3)`,
		name, string(payload), branchID)
	return err
}

func (r *Repository) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]domain.Record, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE outbox_events o
		SET status = 'processing', locked_until = $2, attempts = o.attempts + 1
		WHERE o.id IN (
			SELECT id FROM outbox_events
			WHERE (status = 'pending' AND next_attempt_at <= $1)
			   OR (status = 'processing' AND locked_until < $1)
			ORDER BY next_attempt_at
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+cols, now, now.Add(lease), limit)
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

func (r *Repository) MarkDispatched(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_events SET status = 'dispatched', dispatched_at = $2, locked_until = NULL, last_error = ''
		WHERE id = $1`, id, at)
	return err
}

func (r *Repository) MarkFailed(ctx context.Context, id uuid.UUID, next time.Time, dead bool, errMsg string) error {
	status := domain.StatusPending
	if dead {
		status = domain.StatusDead
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE outbox_events SET status = $2, next_attempt_at = $3, locked_until = NULL, last_error = $4
		WHERE id = $1`, id, status, next, domain.TruncateError(errMsg))
	return err
}

func (r *Repository) Stats(ctx context.Context) (domain.Stats, error) {
	var s domain.Stats
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE status = 'pending'),
		       COUNT(*) FILTER (WHERE status = 'processing'),
		       COUNT(*) FILTER (WHERE status = 'dead'),
		       MIN(next_attempt_at) FILTER (WHERE status = 'pending')
		FROM outbox_events WHERE status <> 'dispatched'`).Scan(&s.Pending, &s.Processing, &s.Dead, &s.OldestDue)
	return s, err
}

func (r *Repository) ListDead(ctx context.Context, limit int) ([]domain.Record, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cols+` FROM outbox_events WHERE status = 'dead'
		ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return scanAll(rows)
}

// Requeue gives a dead record a fresh set of attempts.
func (r *Repository) Requeue(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE outbox_events SET status = 'pending', attempts = 0, next_attempt_at = NOW(), last_error = ''
		WHERE id = $1 AND status = 'dead'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *Repository) PurgeDispatched(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM outbox_events WHERE status = 'dispatched' AND dispatched_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func scanAll(rows pgx.Rows) ([]domain.Record, error) {
	defer rows.Close()
	var out []domain.Record
	for rows.Next() {
		var rec domain.Record
		var status string
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Payload, &rec.BranchID, &status, &rec.Attempts,
			&rec.NextAttemptAt, &rec.LastError, &rec.CreatedAt, &rec.DispatchedAt); err != nil {
			return nil, err
		}
		rec.Status = domain.Status(status)
		out = append(out, rec)
	}
	return out, rows.Err()
}
