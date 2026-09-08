package tx

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ctxKey isolates the active transaction on context.
type ctxKey struct{}

// Manager provides ACID unit-of-work boundaries for application services.
// All repository work inside fn shares the same pgx.Tx when present on context.
type Manager struct {
	pool *pgxpool.Pool
}

func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{pool: pool}
}

// WithinTransaction runs fn inside a DB transaction.
// Commit on success; rollback on error or panic (ACID).
func (m *Manager) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := TxFrom(ctx); ok {
		// Nested call reuses the outer transaction (same unit of work).
		return fn(ctx)
	}

	tx, err := m.pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	txCtx := context.WithValue(ctx, ctxKey{}, tx)

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(txCtx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("rollback after error: %v (original: %w)", rbErr, err)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// TxFrom returns the active transaction if any.
func TxFrom(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(ctxKey{}).(pgx.Tx)
	return tx, ok
}

// Querier is either the pool or an in-flight transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// QuerierFrom returns the transaction when present, otherwise the pool.
func QuerierFrom(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := TxFrom(ctx); ok {
		return tx
	}
	return pool
}
