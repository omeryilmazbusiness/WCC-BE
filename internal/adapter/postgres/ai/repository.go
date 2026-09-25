package ai

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) GetSettings(ctx context.Context, branchID uuid.UUID) (*domain.Settings, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var s domain.Settings
	var cfg []byte
	err := q.QueryRow(ctx, `
		SELECT branch_id, provider, model, enabled, config_json,
		       setup_completed_at, updated_by, updated_at, created_at
		FROM ai_settings WHERE branch_id=$1`, branchID).Scan(
		&s.BranchID, &s.Provider, &s.Model, &s.Enabled, &cfg,
		&s.SetupCompletedAt, &s.UpdatedBy, &s.UpdatedAt, &s.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.ConfigJSON = cfg
	return &s, nil
}

func (r *Repository) UpsertSettings(ctx context.Context, s *domain.Settings) error {
	q := tx.QuerierFrom(ctx, r.pool)
	cfg := s.ConfigJSON
	if len(cfg) == 0 {
		cfg = []byte(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO ai_settings (
			branch_id, provider, model, enabled, config_json,
			setup_completed_at, updated_by, updated_at, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (branch_id) DO UPDATE SET
			provider=EXCLUDED.provider,
			model=EXCLUDED.model,
			enabled=EXCLUDED.enabled,
			config_json=EXCLUDED.config_json,
			setup_completed_at=EXCLUDED.setup_completed_at,
			updated_by=EXCLUDED.updated_by,
			updated_at=EXCLUDED.updated_at`,
		s.BranchID, string(s.Provider), s.Model, s.Enabled, cfg,
		s.SetupCompletedAt, s.UpdatedBy, s.UpdatedAt, s.CreatedAt,
	)
	return err
}

func (r *Repository) InsertRun(ctx context.Context, run *domain.Run) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope := run.ScopeJSON
	if len(scope) == 0 {
		scope = []byte(`{}`)
	}
	out := run.OutputJSON
	if len(out) == 0 {
		out = []byte(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO ai_runs (
			id, branch_id, actor_id, kind, provider, model, scope_json,
			input_hash, output_json, feedback, status, error_message, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		run.ID, run.BranchID, run.ActorID, string(run.Kind), string(run.Provider), run.Model, scope,
		run.InputHash, out, run.Feedback, run.Status, run.ErrorMessage, run.CreatedAt,
	)
	return err
}

func (r *Repository) UpdateRunFeedback(ctx context.Context, id uuid.UUID, feedback string) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE ai_runs SET feedback=$2 WHERE id=$1`, id, feedback)
	return err
}

func (r *Repository) ListRuns(ctx context.Context, branchID uuid.UUID, kind domain.Kind, limit int) ([]domain.Run, error) {
	if limit <= 0 {
		limit = 20
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, actor_id, kind, provider, model, scope_json,
		       input_hash, output_json, feedback, status, error_message, created_at
		FROM ai_runs
		WHERE branch_id=$1 AND ($2 = '' OR kind=$2)
		ORDER BY created_at DESC LIMIT $3`, branchID, string(kind), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Run
	for rows.Next() {
		var run domain.Run
		var scope, output []byte
		if err := rows.Scan(
			&run.ID, &run.BranchID, &run.ActorID, &run.Kind, &run.Provider, &run.Model, &scope,
			&run.InputHash, &output, &run.Feedback, &run.Status, &run.ErrorMessage, &run.CreatedAt,
		); err != nil {
			return nil, err
		}
		run.ScopeJSON = scope
		run.OutputJSON = output
		out = append(out, run)
	}
	return out, rows.Err()
}

// UpdateLeadPriority persists deterministic score on leads.
func (r *Repository) UpdateLeadPriority(ctx context.Context, leadID uuid.UUID, score int, band domain.PriorityBand, signals json.RawMessage) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if len(signals) == 0 {
		signals = []byte(`[]`)
	}
	_, err := q.Exec(ctx, `
		UPDATE leads SET priority_score=$2, priority_band=$3, priority_signals=$4, priority_updated_at=$5
		WHERE id=$1`, leadID, score, string(band), signals, time.Now().UTC())
	return err
}
