package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var scopeTasks = pgscope.Columns{Branch: "branch_id", Owner: "assignee_id"}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const taskCols = `id, branch_id, title, kind, status, priority, outcome, assignee_id,
	COALESCE(related_type, ''), COALESCE(related_id, '00000000-0000-0000-0000-000000000000'::uuid),
	due_at, escalated_at, COALESCE(idempotency_key, ''), source_rule, created_by, overdue_notified_at,
	created_at, updated_at, completed_at,
	COALESCE((SELECT u.full_name FROM users u WHERE u.id = tasks.assignee_id), ''), description,
	package_id, departure_id,
	COALESCE((SELECT p.code FROM packages p WHERE p.id = tasks.package_id), ''),
	COALESCE((SELECT p.name_en FROM packages p WHERE p.id = tasks.package_id), ''),
	COALESCE((SELECT p.name_ar FROM packages p WHERE p.id = tasks.package_id), ''),
	COALESCE((SELECT d.code FROM departures d WHERE d.id = tasks.departure_id), ''),
	(SELECT d.depart_date FROM departures d WHERE d.id = tasks.departure_id)`

const taskFrom = ` FROM tasks`

func (r *Repository) Create(ctx context.Context, t *domain.Task) error {
	if err := pgscope.EnsureBranch(ctx, t.BranchID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	if t.Priority == "" {
		t.Priority = domain.PriorityMinor
	}
	_, err := q.Exec(ctx, `
		INSERT INTO tasks (
			id, branch_id, title, kind, status, priority, outcome, assignee_id, related_type, related_id,
			due_at, escalated_at, idempotency_key, source_rule, created_by, created_at, updated_at, completed_at, description,
			package_id, departure_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		t.ID, t.BranchID, t.Title, t.Kind, t.Status, t.Priority, t.Outcome, t.AssigneeID,
		nullIfEmpty(t.RelatedType), nullUUID(t.RelatedID), t.DueAt, t.EscalatedAt, nullIfEmpty(t.IdempotencyKey), t.SourceRule, t.CreatedBy, t.CreatedAt, t.UpdatedAt, t.CompletedAt,
		t.Description, t.Package.PackageID, t.Package.DepartureID,
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func (r *Repository) Update(ctx context.Context, t *domain.Task) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeTasks, []any{
		t.ID, t.Title, t.Kind, t.Status, t.Priority, t.Outcome, t.AssigneeID,
		t.DueAt, t.EscalatedAt, t.UpdatedAt, t.CompletedAt, t.Description,
		t.Package.PackageID, t.Package.DepartureID,
	})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE tasks SET title=$2, kind=$3, status=$4, priority=$5, outcome=$6, assignee_id=$7,
			overdue_notified_at = CASE WHEN due_at IS DISTINCT FROM $8 THEN NULL ELSE overdue_notified_at END,
			due_at=$8, escalated_at=$9, updated_at=$10, completed_at=$11, description=$12,
			package_id=$13, departure_id=$14
		WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("task")
	}
	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Task, error) {
	return r.findOne(ctx, `id=$1`, id)
}

func (r *Repository) findOne(ctx context.Context, where string, arg any) (*domain.Task, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeTasks, []any{arg})
	if err != nil {
		return nil, err
	}
	return scan(q.QueryRow(ctx, `SELECT `+taskCols+taskFrom+` WHERE `+where+scope, args...))
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Task, error) {
	t, err := r.findOne(ctx, `idempotency_key=$1`, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return t, err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Task, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf("%s=$%d", col, len(args)))
	}
	if f.BranchID != nil {
		add("branch_id", *f.BranchID)
	}
	if f.AssigneeID != nil {
		add("assignee_id", *f.AssigneeID)
	}
	if f.Status != "" {
		add("status", string(f.Status))
	}
	if f.Kind != "" {
		add("kind", string(f.Kind))
	}
	if f.RelatedType != "" && f.RelatedID != nil {
		add("related_type", f.RelatedType)
		add("related_id", *f.RelatedID)
	}
	if f.PackageID != nil {
		add("package_id", *f.PackageID)
	}
	if f.DepartureID != nil {
		add("departure_id", *f.DepartureID)
	}
	if f.OverdueOnly {
		where = append(where, `status IN ('open','in_progress') AND due_at IS NOT NULL AND due_at < NOW()`)
	}
	if f.EscalatedOnly {
		where = append(where, `escalated_at IS NOT NULL`)
	}
	if qs := strings.TrimSpace(f.Query); qs != "" {
		args = append(args, "%"+qs+"%")
		where = append(where, fmt.Sprintf(`(title ILIKE $%d OR description ILIKE $%d OR CAST(id AS TEXT) ILIKE $%d)`, len(args), len(args), len(args)))
	}
	where, args, err := pgscope.Append(ctx, scopeTasks, where, args)
	if err != nil {
		return nil, 0, err
	}
	i := len(args) + 1
	clause := strings.Join(where, " AND ")
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := fmt.Sprintf(`SELECT %s%s WHERE %s
		ORDER BY CASE WHEN escalated_at IS NOT NULL THEN 0 ELSE 1 END,
			CASE priority WHEN 'critical' THEN 0 WHEN 'major' THEN 1 ELSE 2 END,
			due_at NULLS LAST, created_at DESC
		LIMIT $%d OFFSET $%d`, taskCols, taskFrom, clause, i, i+1)
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
	scope, args, err := pgscope.Clause(ctx, scopeTasks, []any{branchID})
	if err != nil {
		return 0, err
	}
	var n int
	err = q.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE status IN ('open','in_progress') AND due_at < NOW()
		  AND ($1::uuid IS NULL OR branch_id=$1)`+scope, args...).Scan(&n)
	return n, err
}

func (r *Repository) ListOpenByRule(ctx context.Context, rule, relatedType string, relatedID uuid.UUID) ([]domain.Task, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeTasks, []any{rule, relatedType, relatedID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+taskCols+taskFrom+`
		WHERE source_rule=$1 AND related_type=$2 AND related_id=$3
		  AND status IN ('open','in_progress')`+scope, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

func (r *Repository) ListOverdueUnnotified(ctx context.Context, now time.Time, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 200
	}
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeTasks, []any{now, limit})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+taskCols+taskFrom+`
		WHERE status IN ('open','in_progress') AND due_at IS NOT NULL AND due_at < $1
		  AND overdue_notified_at IS NULL`+scope+`
		ORDER BY due_at ASC LIMIT $2`, args...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

func (r *Repository) MarkOverdueNotified(ctx context.Context, id uuid.UUID, at time.Time) (bool, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeTasks, []any{id, at})
	if err != nil {
		return false, err
	}
	tag, err := q.Exec(ctx, `UPDATE tasks SET overdue_notified_at=$2
		WHERE id=$1 AND overdue_notified_at IS NULL`+scope, args...)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func collect(rows pgx.Rows) ([]domain.Task, error) {
	defer rows.Close()
	var out []domain.Task
	for rows.Next() {
		t, err := scanRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func scan(row pgx.Row) (*domain.Task, error) {
	var t domain.Task
	var kind, status, priority string
	err := row.Scan(
		&t.ID, &t.BranchID, &t.Title, &kind, &status, &priority, &t.Outcome, &t.AssigneeID,
		&t.RelatedType, &t.RelatedID, &t.DueAt, &t.EscalatedAt, &t.IdempotencyKey,
		&t.SourceRule, &t.CreatedBy, &t.OverdueNotifiedAt,
		&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt, &t.AssigneeName, &t.Description,
		&t.Package.PackageID, &t.Package.DepartureID,
		&t.Package.PackageCode, &t.Package.PackageName, &t.Package.PackageNameAr, &t.Package.DepartureCode, &t.Package.DepartDate,
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
		&t.SourceRule, &t.CreatedBy, &t.OverdueNotifiedAt,
		&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt, &t.AssigneeName, &t.Description,
		&t.Package.PackageID, &t.Package.DepartureID,
		&t.Package.PackageCode, &t.Package.PackageName, &t.Package.PackageNameAr, &t.Package.DepartureCode, &t.Package.DepartDate,
	)
	if err != nil {
		return nil, err
	}
	t.Kind = domain.Kind(kind)
	t.Status = domain.Status(status)
	t.Priority = domain.Priority(priority)
	return &t, nil
}
