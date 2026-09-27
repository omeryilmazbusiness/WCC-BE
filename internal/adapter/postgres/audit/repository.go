package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Repository only ever INSERTs into audit_events; the table rejects
// UPDATE, DELETE and TRUNCATE (migration 00025).
type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const selectEvents = `
	SELECT a.id, a.actor_id, COALESCE(u.full_name, ''), a.actor_type, a.action, a.entity_type, a.entity_id,
	       a.branch_id, a.before, a.after, a.metadata, a.ip, a.user_agent, a.session_id, a.request_id, a.created_at
	FROM audit_events a
	LEFT JOIN users u ON u.id = a.actor_id`

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
	if e.ActorType == "" {
		e.ActorType = domain.ActorSystem
		if e.ActorID != nil {
			e.ActorType = domain.ActorUser
		}
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO audit_events (id, actor_id, actor_type, action, entity_type, entity_id, branch_id,
		                          before, after, metadata, ip, user_agent, session_id, request_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		e.ID, e.ActorID, string(e.ActorType), e.Action, e.EntityType, e.EntityID, e.BranchID,
		nullJSON(e.Before), nullJSON(e.After), e.Metadata, e.IP, e.UserAgent, e.SessionID, e.RequestID, e.CreatedAt,
	)
	return err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Event, int64, error) {
	w, args, err := filterWhere(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var total int64
	if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM audit_events a WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 25
	}
	n := len(args)
	args = append(args, limit, f.Offset)
	rows, err := q.Query(ctx, selectEvents+` WHERE `+w+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $`+fmt.Sprint(n+1)+` OFFSET $`+fmt.Sprint(n+2), args...)
	if err != nil {
		return nil, 0, err
	}
	out := []domain.Event{}
	err = scanEvents(rows, func(e domain.Event) error {
		out = append(out, e)
		return nil
	})
	return out, total, err
}

func (r *Repository) Stream(ctx context.Context, f domain.ListFilter, max int, fn func(domain.Event) error) error {
	w, args, err := filterWhere(ctx, f)
	if err != nil {
		return err
	}
	args = append(args, max)
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, selectEvents+` WHERE `+w+`
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return err
	}
	return scanEvents(rows, fn)
}

func (r *Repository) ListActions(ctx context.Context) ([]string, error) {
	where, args, err := pgscope.Append(ctx, pgscope.Columns{Branch: "branch_id"}, []string{"1=1"}, nil)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx,
		"SELECT DISTINCT action FROM audit_events WHERE "+strings.Join(where, " AND ")+" ORDER BY action", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func filterWhere(ctx context.Context, f domain.ListFilter) (string, []any, error) {
	where := []string{"1=1"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.ActorID != nil {
		add("a.actor_id=$%d", *f.ActorID)
	}
	if f.EntityType != "" {
		add("a.entity_type=$%d", f.EntityType)
	}
	if f.EntityID != nil {
		add("a.entity_id=$%d", *f.EntityID)
	}
	if f.Action != "" {
		add("a.action=$%d", f.Action)
	}
	if f.BranchID != nil {
		add("a.branch_id=$%d", *f.BranchID)
	}
	if f.From != nil {
		add("a.created_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("a.created_at <= $%d", *f.To)
	}
	where, args, err := pgscope.Append(ctx, pgscope.Columns{Branch: "a.branch_id"}, where, args)
	if err != nil {
		return "", nil, err
	}
	return strings.Join(where, " AND "), args, nil
}

func scanEvents(rows pgx.Rows, fn func(domain.Event) error) error {
	defer rows.Close()
	for rows.Next() {
		var e domain.Event
		var actorType string
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorName, &actorType, &e.Action, &e.EntityType, &e.EntityID,
			&e.BranchID, &e.Before, &e.After, &e.Metadata, &e.IP, &e.UserAgent, &e.SessionID, &e.RequestID, &e.CreatedAt); err != nil {
			return err
		}
		e.ActorType = domain.ActorType(actorType)
		if err := fn(e); err != nil {
			return err
		}
	}
	return rows.Err()
}

func nullJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
