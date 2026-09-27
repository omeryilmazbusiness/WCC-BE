// Package pgbatch runs large retention deletes in short statements so they
// never hold long row locks or bloat a single transaction.
package pgbatch

import (
	"context"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Size is the row limit per statement.
const Size = 5000

// Delete repeats sql until it affects fewer than Size rows. sql must delete
// at most Size rows per run, e.g.
// DELETE FROM t WHERE id IN (SELECT id FROM t WHERE ... LIMIT 5000).
func Delete(ctx context.Context, q tx.Querier, sql string, args ...any) (int64, error) {
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		tag, err := q.Exec(ctx, sql, args...)
		if err != nil {
			return total, err
		}
		n := tag.RowsAffected()
		total += n
		if n < Size {
			return total, nil
		}
	}
}
