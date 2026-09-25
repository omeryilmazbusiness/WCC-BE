package extint

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/extint"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) List(ctx context.Context, branchID uuid.UUID) ([]domain.Integration, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, kind, provider_key, display_name, status, health,
			config_json, last_checked_at, last_error, created_at, updated_at
		FROM external_integrations
		WHERE branch_id=$1
		ORDER BY kind, provider_key`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Integration
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

func (r *Repository) Get(ctx context.Context, branchID, id uuid.UUID) (*domain.Integration, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, kind, provider_key, display_name, status, health,
			config_json, last_checked_at, last_error, created_at, updated_at
		FROM external_integrations
		WHERE branch_id=$1 AND id=$2`, branchID, id)
	i, err := scanIntegrationRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return i, err
}

func (r *Repository) Upsert(ctx context.Context, i *domain.Integration) error {
	q := tx.QuerierFrom(ctx, r.pool)
	cfg := i.ConfigJSON
	if len(cfg) == 0 {
		cfg = []byte(`{}`)
	}
	err := q.QueryRow(ctx, `
		INSERT INTO external_integrations (
			id, branch_id, kind, provider_key, display_name, status, health,
			config_json, last_checked_at, last_error, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (branch_id, kind, provider_key) DO UPDATE SET
			display_name=EXCLUDED.display_name,
			status=EXCLUDED.status,
			health=EXCLUDED.health,
			config_json=EXCLUDED.config_json,
			last_checked_at=EXCLUDED.last_checked_at,
			last_error=EXCLUDED.last_error,
			updated_at=EXCLUDED.updated_at
		RETURNING id`,
		i.ID, i.BranchID, string(i.Kind), i.ProviderKey, i.DisplayName,
		string(i.Status), string(i.Health), cfg, i.LastCheckedAt, i.LastError,
		i.CreatedAt, i.UpdatedAt,
	).Scan(&i.ID)
	return err
}

func (r *Repository) Update(ctx context.Context, i *domain.Integration) error {
	q := tx.QuerierFrom(ctx, r.pool)
	cfg := i.ConfigJSON
	if len(cfg) == 0 {
		cfg = []byte(`{}`)
	}
	ct, err := q.Exec(ctx, `
		UPDATE external_integrations SET
			display_name=$3, status=$4, health=$5, config_json=$6,
			last_checked_at=$7, last_error=$8, updated_at=$9
		WHERE branch_id=$1 AND id=$2`,
		i.BranchID, i.ID, i.DisplayName, string(i.Status), string(i.Health),
		cfg, i.LastCheckedAt, i.LastError, i.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, branchID, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `DELETE FROM external_integrations WHERE branch_id=$1 AND id=$2`, branchID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func scanIntegrationRow(row pgx.Row) (*domain.Integration, error) {
	var i domain.Integration
	var kind, status, health string
	var cfg []byte
	err := row.Scan(
		&i.ID, &i.BranchID, &kind, &i.ProviderKey, &i.DisplayName, &status, &health,
		&cfg, &i.LastCheckedAt, &i.LastError, &i.CreatedAt, &i.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	i.Kind = domain.Kind(kind)
	i.Status = domain.Status(status)
	i.Health = domain.Health(health)
	i.ConfigJSON = cfg
	return &i, nil
}

func scanIntegration(rows pgx.Rows) (*domain.Integration, error) {
	var i domain.Integration
	var kind, status, health string
	var cfg []byte
	err := rows.Scan(
		&i.ID, &i.BranchID, &kind, &i.ProviderKey, &i.DisplayName, &status, &health,
		&cfg, &i.LastCheckedAt, &i.LastError, &i.CreatedAt, &i.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	i.Kind = domain.Kind(kind)
	i.Status = domain.Status(status)
	i.Health = domain.Health(health)
	i.ConfigJSON = cfg
	return &i, nil
}

var _ domain.Repository = (*Repository)(nil)
