package task

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Participation in a thread is decided by the service from the task; the
// repository only keeps notes inside the caller's branch reach.
var scopeNotes = pgscope.Columns{Branch: "branch_id"}

func (r *Repository) CreateNote(ctx context.Context, n *domain.Note) error {
	if err := pgscope.EnsureBranch(ctx, n.BranchID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO task_notes (id, task_id, branch_id, author_id, recipient_id, body, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		n.ID, n.TaskID, n.BranchID, n.AuthorID, n.RecipientID, n.Body, n.CreatedAt,
	)
	return err
}

func (r *Repository) ListNotes(ctx context.Context, taskID uuid.UUID, limit int) ([]domain.Note, error) {
	if limit <= 0 || limit > domain.MaxNotesPerThread {
		limit = domain.MaxNotesPerThread
	}
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeNotes, []any{taskID, limit})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT id, task_id, branch_id, author_id, recipient_id, body, created_at, read_at,
			COALESCE((SELECT u.full_name FROM users u WHERE u.id = task_notes.author_id), ''),
			COALESCE((SELECT u.full_name FROM users u WHERE u.id = task_notes.recipient_id), '')
		FROM (
			SELECT * FROM task_notes WHERE task_id=$1`+scope+`
			ORDER BY created_at DESC, id DESC LIMIT $2
		) task_notes
		ORDER BY created_at ASC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Note{}
	for rows.Next() {
		var n domain.Note
		if err := rows.Scan(&n.ID, &n.TaskID, &n.BranchID, &n.AuthorID, &n.RecipientID, &n.Body,
			&n.CreatedAt, &n.ReadAt, &n.AuthorName, &n.RecipientName); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *Repository) MarkNotesRead(ctx context.Context, taskID, recipientID uuid.UUID, at time.Time) (int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := pgscope.Clause(ctx, scopeNotes, []any{taskID, recipientID, at})
	if err != nil {
		return 0, err
	}
	tag, err := q.Exec(ctx, `UPDATE task_notes SET read_at=$3
		WHERE task_id=$1 AND recipient_id=$2 AND read_at IS NULL`+scope, args...)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
