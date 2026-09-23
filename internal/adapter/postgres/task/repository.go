package task

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

const taskCols = `id, branch_id, title, kind, status, priority, outcome, assignee_id, related_type, related_id,
	due_at, escalated_at, idempotency_key, created_at, updated_at, completed_at`

func (r *Repository) Create(ctx context.Context, t *domain.Task) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if t.Priority == "" {
		t.Priority = domain.PriorityNormal
	}
	_, err := q.Exec(ctx, `
		INSERT INTO tasks (
			id, branch_id, title, kind, status, priority, outcome, assignee_id, related_type, related_id,
			due_at, escalated_at, idempotency_key, created_at, updated_at, completed_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		t.ID, t.BranchID, t.Title, t.Kind, t.Status, t.Priority, t.Outcome, t.AssigneeID, t.RelatedType, t.RelatedID,
		t.DueAt, t.EscalatedAt, nullIfEmpty(t.IdempotencyKey), t.CreatedAt, t.UpdatedAt, t.CompletedAt,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (r *Repository) Update(ctx context.Context, t *domain.Task) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE tasks SET title=$2, kind=$3, status=$4, priority=$5, outcome=$6, assignee_id=$7,
			due_at=$8, escalated_at=$9, updated_at=$10, completed_at=$11
		WHERE id=$1`,
		t.ID, t.Title, t.Kind, t.Status, t.Priority, t.Outcome, t.AssigneeID,
		t.DueAt, t.EscalatedAt, t.UpdatedAt, t.CompletedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	return scan(q.QueryRow(ctx, `SELECT `+taskCols+` FROM tasks WHERE id=$1`, id))
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Task, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	t, err := scan(q.QueryRow(ctx, `SELECT `+taskCols+` FROM tasks WHERE idempotency_key=$1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return t, err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Task, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	i := 1
	if f.BranchID != nil {
		where = append(where, fmt.Sprintf("branch_id=$%d", i))
		args = append(args, *f.BranchID)
		i++
	}
	if f.AssigneeID != nil {
		where = append(where, fmt.Sprintf("assignee_id=$%d", i))
		args = append(args, *f.AssigneeID)
		i++
	}
	if f.Status != "" {
		where = append(where, fmt.Sprintf("status=$%d", i))
		args = append(args, string(f.Status))
		i++
	}
	if f.Kind != "" {
		where = append(where, fmt.Sprintf("kind=$%d", i))
		args = append(args, string(f.Kind))
		i++
	}
	if f.RelatedType != "" && f.RelatedID != nil {
		where = append(where, fmt.Sprintf("related_type=$%d AND related_id=$%d", i, i+1))
		args = append(args, f.RelatedType, *f.RelatedID)
		i += 2
	}
	if f.OverdueOnly {
		where = append(where, `status IN ('open','in_progress') AND due_at IS NOT NULL AND due_at < NOW()`)
	}
	if f.EscalatedOnly {
		where = append(where, `escalated_at IS NOT NULL`)
	}
	if qs := strings.TrimSpace(f.Query); qs != "" {
		where = append(where, fmt.Sprintf(`(title ILIKE $%d OR CAST(id AS TEXT) ILIKE $%d)`, i, i))
		args = append(args, "%"+qs+"%")
		i++
	}
	clause := strings.Join(where, " AND ")
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := fmt.Sprintf(`SELECT %s FROM tasks WHERE %s
		ORDER BY CASE WHEN escalated_at IS NOT NULL THEN 0 ELSE 1 END,
			CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 WHEN 'normal' THEN 2 ELSE 3 END,
			due_at NULLS LAST, created_at DESC
		LIMIT $%d OFFSET $%d`, taskCols, clause, i, i+1)
	args = append(args, limit, offset)
	rows, err := q.Query(ctx, listSQL, args...)
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

func (r *Repository) ListByAssignee(ctx context.Context, assigneeID uuid.UUID, status *domain.Status, limit, offset int) ([]domain.Task, int, error) {
	f := domain.ListFilter{AssigneeID: &assigneeID, Limit: limit, Offset: offset}
	if status != nil {
		f.Status = *status
	}
	return r.List(ctx, f)
}

func (r *Repository) ListByRelated(ctx context.Context, relatedType string, relatedID uuid.UUID) ([]domain.Task, error) {
	items, _, err := r.List(ctx, domain.ListFilter{
		RelatedType: relatedType, RelatedID: &relatedID, Limit: 200,
	})
	return items, err
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
	var kind, status, priority string
	err := row.Scan(
		&t.ID, &t.BranchID, &t.Title, &kind, &status, &priority, &t.Outcome, &t.AssigneeID,
		&t.RelatedType, &t.RelatedID, &t.DueAt, &t.EscalatedAt, &t.IdempotencyKey,
		&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	t.Kind = domain.Kind(kind)
	t.Status = domain.Status(status)
	t.Priority = domain.Priority(priority)
	return &t, nil
}

func scanRows(rows pgx.Rows) (*domain.Task, error) {
	var t domain.Task
	var kind, status, priority string
	err := rows.Scan(
		&t.ID, &t.BranchID, &t.Title, &kind, &status, &priority, &t.Outcome, &t.AssigneeID,
		&t.RelatedType, &t.RelatedID, &t.DueAt, &t.EscalatedAt, &t.IdempotencyKey,
		&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
	)
	if err != nil {
		return nil, err
	}
	t.Kind = domain.Kind(kind)
	t.Status = domain.Status(status)
	t.Priority = domain.Priority(priority)
	return &t, nil
}
