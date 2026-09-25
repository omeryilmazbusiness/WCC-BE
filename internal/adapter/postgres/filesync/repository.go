package filesync

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/filesync"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListConnections(ctx context.Context, branchID uuid.UUID) ([]domain.Connection, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, provider, display_name, remote_path, entity_type,
			source_of_truth, conflict_policy, enabled, status, last_sync_at, last_error,
			config_json, created_by, created_at, updated_at
		FROM file_sync_connections
		WHERE branch_id=$1
		ORDER BY created_at DESC`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConnections(rows)
}

func (r *Repository) GetConnection(ctx context.Context, branchID, id uuid.UUID) (*domain.Connection, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, provider, display_name, remote_path, entity_type,
			source_of_truth, conflict_policy, enabled, status, last_sync_at, last_error,
			config_json, created_by, created_at, updated_at
		FROM file_sync_connections
		WHERE branch_id=$1 AND id=$2`, branchID, id)
	c, err := scanConnection(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return c, err
}

func (r *Repository) InsertConnection(ctx context.Context, c *domain.Connection) error {
	q := tx.QuerierFrom(ctx, r.pool)
	cfg := c.ConfigJSON
	if len(cfg) == 0 {
		cfg = []byte(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO file_sync_connections (
			id, branch_id, provider, display_name, remote_path, entity_type,
			source_of_truth, conflict_policy, enabled, status, last_sync_at, last_error,
			config_json, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		c.ID, c.BranchID, string(c.Provider), c.DisplayName, c.RemotePath, c.EntityType,
		string(c.SourceOfTruth), string(c.ConflictPolicy), c.Enabled, string(c.Status),
		c.LastSyncAt, c.LastError, cfg, c.CreatedBy, c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateConnection(ctx context.Context, c *domain.Connection) error {
	q := tx.QuerierFrom(ctx, r.pool)
	cfg := c.ConfigJSON
	if len(cfg) == 0 {
		cfg = []byte(`{}`)
	}
	ct, err := q.Exec(ctx, `
		UPDATE file_sync_connections SET
			display_name=$3, remote_path=$4, entity_type=$5, source_of_truth=$6,
			conflict_policy=$7, enabled=$8, status=$9, last_sync_at=$10, last_error=$11,
			config_json=$12, updated_at=$13
		WHERE branch_id=$1 AND id=$2`,
		c.BranchID, c.ID, c.DisplayName, c.RemotePath, c.EntityType, string(c.SourceOfTruth),
		string(c.ConflictPolicy), c.Enabled, string(c.Status), c.LastSyncAt, c.LastError,
		cfg, c.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) DeleteConnection(ctx context.Context, branchID, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `DELETE FROM file_sync_connections WHERE branch_id=$1 AND id=$2`, branchID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) InsertRun(ctx context.Context, run *domain.Run) error {
	q := tx.QuerierFrom(ctx, r.pool)
	sum := run.SummaryJSON
	if len(sum) == 0 {
		sum = []byte(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO file_sync_runs (
			id, connection_id, branch_id, actor_id, direction, status,
			rows_read, rows_applied, conflicts, summary_json, error_message,
			started_at, finished_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		run.ID, run.ConnectionID, run.BranchID, run.ActorID, string(run.Direction), string(run.Status),
		run.RowsRead, run.RowsApplied, run.Conflicts, sum, run.ErrorMessage,
		run.StartedAt, run.FinishedAt,
	)
	return err
}

func (r *Repository) UpdateRun(ctx context.Context, run *domain.Run) error {
	q := tx.QuerierFrom(ctx, r.pool)
	sum := run.SummaryJSON
	if len(sum) == 0 {
		sum = []byte(`{}`)
	}
	ct, err := q.Exec(ctx, `
		UPDATE file_sync_runs SET
			status=$2, rows_read=$3, rows_applied=$4, conflicts=$5,
			summary_json=$6, error_message=$7, finished_at=$8
		WHERE id=$1`,
		run.ID, string(run.Status), run.RowsRead, run.RowsApplied, run.Conflicts,
		sum, run.ErrorMessage, run.FinishedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) ListRuns(ctx context.Context, branchID uuid.UUID, connectionID *uuid.UUID, limit int) ([]domain.Run, error) {
	if limit <= 0 {
		limit = 50
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, connection_id, branch_id, actor_id, direction, status,
			rows_read, rows_applied, conflicts, summary_json, error_message,
			started_at, finished_at
		FROM file_sync_runs
		WHERE branch_id=$1 AND ($2::uuid IS NULL OR connection_id=$2)
		ORDER BY started_at DESC
		LIMIT $3`, branchID, connectionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		var run domain.Run
		var dir, status string
		var sum []byte
		if err := rows.Scan(
			&run.ID, &run.ConnectionID, &run.BranchID, &run.ActorID, &dir, &status,
			&run.RowsRead, &run.RowsApplied, &run.Conflicts, &sum, &run.ErrorMessage,
			&run.StartedAt, &run.FinishedAt,
		); err != nil {
			return nil, err
		}
		run.Direction = domain.Direction(dir)
		run.Status = domain.RunStatus(status)
		run.SummaryJSON = sum
		out = append(out, run)
	}
	return out, rows.Err()
}

func scanConnection(row pgx.Row) (*domain.Connection, error) {
	var c domain.Connection
	var provider, truth, policy, status string
	var cfg []byte
	err := row.Scan(
		&c.ID, &c.BranchID, &provider, &c.DisplayName, &c.RemotePath, &c.EntityType,
		&truth, &policy, &c.Enabled, &status, &c.LastSyncAt, &c.LastError,
		&cfg, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	c.Provider = domain.Provider(provider)
	c.SourceOfTruth = domain.SourceOfTruth(truth)
	c.ConflictPolicy = domain.ConflictPolicy(policy)
	c.Status = domain.ConnStatus(status)
	c.ConfigJSON = cfg
	return &c, nil
}

func scanConnections(rows pgx.Rows) ([]domain.Connection, error) {
	var out []domain.Connection
	for rows.Next() {
		var c domain.Connection
		var provider, truth, policy, status string
		var cfg []byte
		if err := rows.Scan(
			&c.ID, &c.BranchID, &provider, &c.DisplayName, &c.RemotePath, &c.EntityType,
			&truth, &policy, &c.Enabled, &status, &c.LastSyncAt, &c.LastError,
			&cfg, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		c.Provider = domain.Provider(provider)
		c.SourceOfTruth = domain.SourceOfTruth(truth)
		c.ConflictPolicy = domain.ConflictPolicy(policy)
		c.Status = domain.ConnStatus(status)
		c.ConfigJSON = cfg
		out = append(out, c)
	}
	return out, rows.Err()
}

var _ domain.Repository = (*Repository)(nil)
