package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, t *domain.Task) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO tasks (
			id, branch_id, title, kind, status, assignee_id, related_type, related_id,
			due_at, idempotency_key, created_at, updated_at, completed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		t.ID, t.BranchID, t.Title, t.Kind, t.Status, t.AssigneeID, t.RelatedType, t.RelatedID,
		t.DueAt, t.IdempotencyKey, t.CreatedAt, t.UpdatedAt, t.CompletedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, t *domain.Task) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE tasks SET title=$2, kind=$3, status=$4, assignee_id=$5, due_at=$6, updated_at=$7, completed_at=$8
		WHERE id=$1`,
		t.ID, t.Title, t.Kind, t.Status, t.AssigneeID, t.DueAt, t.UpdatedAt, t.CompletedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, title, kind, status, assignee_id, related_type, related_id,
			due_at, idempotency_key, created_at, updated_at, completed_at
		FROM tasks WHERE id=$1`, id)
	return scan(row)
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Task, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, title, kind, status, assignee_id, related_type, related_id,
			due_at, idempotency_key, created_at, updated_at, completed_at
		FROM tasks WHERE idempotency_key=$1`, key)
	t, err := scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return t, err
}

func (r *Repository) ListByAssignee(ctx context.Context, assigneeID uuid.UUID, status *domain.Status, limit, offset int) ([]domain.Task, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var total int
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks WHERE assignee_id=$1 AND ($2::text IS NULL OR status=$2)`,
		assigneeID, status).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, title, kind, status, assignee_id, related_type, related_id,
			due_at, idempotency_key, created_at, updated_at, completed_at
		FROM tasks WHERE assignee_id=$1 AND ($2::text IS NULL OR status=$2)
		ORDER BY due_at NULLS LAST, created_at DESC
		LIMIT $3 OFFSET $4`, assigneeID, status, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanRows(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

func (r *Repository) CountOverdue(ctx context.Context, branchID *uuid.UUID) (int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var n int
	err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE status IN ('open','in_progress') AND due_at < NOW()
		  AND ($1::uuid IS NULL OR branch_id=$1)`, branchID).Scan(&n)
	return n, err
}

func scan(row pgx.Row) (*domain.Task, error) {
	var t domain.Task
	var kind, status string
	err := row.Scan(&t.ID, &t.BranchID, &t.Title, &kind, &status, &t.AssigneeID, &t.RelatedType, &t.RelatedID,
		&t.DueAt, &t.IdempotencyKey, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	t.Kind = domain.Kind(kind)
	t.Status = domain.Status(status)
	return &t, nil
}

func scanRows(rows pgx.Rows) (*domain.Task, error) {
	var t domain.Task
	var kind, status string
	err := rows.Scan(&t.ID, &t.BranchID, &t.Title, &kind, &status, &t.AssigneeID, &t.RelatedType, &t.RelatedID,
		&t.DueAt, &t.IdempotencyKey, &t.CreatedAt, &t.UpdatedAt, &t.CompletedAt)
	if err != nil {
		return nil, err
	}
	t.Kind = domain.Kind(kind)
	t.Status = domain.Status(status)
	return &t, nil
}
