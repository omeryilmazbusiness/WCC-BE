package schedule

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Ledger implements shared.RunLedger on scheduled_job_runs.
type Ledger struct {
	pool *pgxpool.Pool
}

func NewLedger(pool *pgxpool.Pool) *Ledger { return &Ledger{pool: pool} }

func (l *Ledger) Claim(ctx context.Context, job, scopeKey, period string) (bool, error) {
	tag, err := tx.QuerierFrom(ctx, l.pool).Exec(ctx, `
		INSERT INTO scheduled_job_runs (job, scope_key, period) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, job, scopeKey, period)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
