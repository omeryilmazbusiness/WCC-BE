package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Insert(ctx context.Context, e *domain.Event) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Metadata == nil {
		e.Metadata = []byte("{}")
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO audit_events (id, actor_id, action, entity_type, entity_id, branch_id, metadata, ip, user_agent, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		e.ID, e.ActorID, e.Action, e.EntityType, e.EntityID, e.BranchID, e.Metadata, e.IP, e.UserAgent, e.CreatedAt,
	)
	return err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Event, int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	i := 1
	if f.ActorID != nil {
		where = append(where, fmt.Sprintf("actor_id=$%d", i))
		args = append(args, *f.ActorID)
		i++
	}
	if f.EntityType != "" {
		where = append(where, fmt.Sprintf("entity_type=$%d", i))
		args = append(args, f.EntityType)
		i++
	}
	if f.EntityID != nil {
		where = append(where, fmt.Sprintf("entity_id=$%d", i))
		args = append(args, *f.EntityID)
		i++
	}
	if f.Action != "" {
		where = append(where, fmt.Sprintf("action=$%d", i))
		args = append(args, f.Action)
		i++
	}
	if f.BranchID != nil {
		where = append(where, fmt.Sprintf("branch_id=$%d", i))
		args = append(args, *f.BranchID)
		i++
	}
	if f.From != nil {
		where = append(where, fmt.Sprintf("created_at >= $%d", i))
		args = append(args, *f.From)
		i++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("created_at <= $%d", i))
		args = append(args, *f.To)
		i++
	}
	w := strings.Join(where, " AND ")
	var total int64
	if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM audit_events WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 25
	}
	args = append(args, limit, f.Offset)
	rows, err := q.Query(ctx, `
		SELECT id, actor_id, action, entity_type, entity_id, branch_id, metadata, ip, user_agent, created_at
		FROM audit_events WHERE `+w+`
		ORDER BY created_at DESC
		LIMIT $`+fmt.Sprint(i)+` OFFSET $`+fmt.Sprint(i+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.EntityType, &e.EntityID, &e.BranchID, &e.Metadata, &e.IP, &e.UserAgent, &e.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
