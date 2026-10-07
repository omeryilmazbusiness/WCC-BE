// Package assistant stores the in-app assistant's per-user daily usage.
package assistant

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// UsageRepository counts model answers per user and day. Rows are keyed by the
// authenticated user, so no branch scope clause is needed to read one's own.
type UsageRepository struct {
	pool *pgxpool.Pool
}

func NewUsageRepository(pool *pgxpool.Pool) *UsageRepository {
	return &UsageRepository{pool: pool}
}

func (r *UsageRepository) Used(ctx context.Context, userID uuid.UUID, day time.Time) (int, error) {
	var n int
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT COALESCE((SELECT llm_calls FROM assistant_usage WHERE user_id=$1 AND day=$2), 0)`,
		userID, day.Format(time.DateOnly)).Scan(&n)
	return n, err
}

// Add counts one model answer atomically, so parallel tabs cannot lose increments.
func (r *UsageRepository) Add(ctx context.Context, userID, branchID uuid.UUID, day time.Time, tokens int) error {
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO assistant_usage (user_id, day, branch_id, llm_calls, tokens_est)
		VALUES ($1, $2, $3, 1, $4)
		ON CONFLICT (user_id, day) DO UPDATE
		SET llm_calls = assistant_usage.llm_calls + 1,
		    tokens_est = assistant_usage.tokens_est + EXCLUDED.tokens_est,
		    branch_id = EXCLUDED.branch_id,
		    updated_at = NOW()`,
		userID, day.Format(time.DateOnly), branchID, max(0, tokens))
	return err
}
