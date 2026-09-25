package notification

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const cols = `id, branch_id, recipient_user_id, kind, severity, title, body,
	entity_type, entity_id, group_key, occurrence_count, status, href_hint, meta_json,
	created_at, updated_at, acknowledged_at, acknowledged_by, resolved_at, resolved_by`

func scan(row pgx.Row) (*domain.Notification, error) {
	var n domain.Notification
	var meta []byte
	err := row.Scan(
		&n.ID, &n.BranchID, &n.RecipientUserID, &n.Kind, &n.Severity, &n.Title, &n.Body,
		&n.EntityType, &n.EntityID, &n.GroupKey, &n.OccurrenceCount, &n.Status, &n.HrefHint, &meta,
		&n.CreatedAt, &n.UpdatedAt, &n.AcknowledgedAt, &n.AcknowledgedBy, &n.ResolvedAt, &n.ResolvedBy,
	)
	if err != nil {
		return nil, err
	}
	if len(meta) == 0 {
		n.MetaJSON = json.RawMessage(`{}`)
	} else {
		n.MetaJSON = meta
	}
	return &n, nil
}

func (r *Repository) Create(ctx context.Context, n *domain.Notification) error {
	q := tx.QuerierFrom(ctx, r.pool)
	meta := n.MetaJSON
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO notifications (
			id, branch_id, recipient_user_id, kind, severity, title, body,
			entity_type, entity_id, group_key, occurrence_count, status, href_hint, meta_json,
			created_at, updated_at, acknowledged_at, acknowledged_by, resolved_at, resolved_by
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
		)`,
		n.ID, n.BranchID, n.RecipientUserID, n.Kind, string(n.Severity), n.Title, n.Body,
		n.EntityType, n.EntityID, n.GroupKey, n.OccurrenceCount, string(n.Status), n.HrefHint, meta,
		n.CreatedAt, n.UpdatedAt, n.AcknowledgedAt, n.AcknowledgedBy, n.ResolvedAt, n.ResolvedBy,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, n *domain.Notification) error {
	q := tx.QuerierFrom(ctx, r.pool)
	meta := n.MetaJSON
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	_, err := q.Exec(ctx, `
		UPDATE notifications SET
			severity=$2, title=$3, body=$4, occurrence_count=$5, status=$6, href_hint=$7, meta_json=$8,
			updated_at=$9, acknowledged_at=$10, acknowledged_by=$11, resolved_at=$12, resolved_by=$13
		WHERE id=$1`,
		n.ID, string(n.Severity), n.Title, n.Body, n.OccurrenceCount, string(n.Status), n.HrefHint, meta,
		n.UpdatedAt, n.AcknowledgedAt, n.AcknowledgedBy, n.ResolvedAt, n.ResolvedBy,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Notification, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	n, err := scan(q.QueryRow(ctx, `SELECT `+cols+` FROM notifications WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

func (r *Repository) FindOpenByGroup(ctx context.Context, recipientUserID uuid.UUID, groupKey string) (*domain.Notification, error) {
	if groupKey == "" {
		return nil, nil
	}
	q := tx.QuerierFrom(ctx, r.pool)
	n, err := scan(q.QueryRow(ctx, `
		SELECT `+cols+` FROM notifications
		WHERE recipient_user_id=$1 AND group_key=$2 AND status='open'
		ORDER BY updated_at DESC LIMIT 1`, recipientUserID, groupKey))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Notification, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	where := `recipient_user_id=$1`
	args := []any{f.RecipientUserID}
	i := 2
	if f.Status != "" {
		where += ` AND status=$` + strconv.Itoa(i)
		args = append(args, string(f.Status))
		i++
	} else if !f.IncludeResolved {
		where += ` AND status IN ('open','acknowledged')`
	}
	if len(f.Kinds) > 0 {
		where += ` AND kind = ANY($` + strconv.Itoa(i) + `)`
		args = append(args, f.Kinds)
		i++
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, f.Offset)
	rows, err := q.Query(ctx, `
		SELECT `+cols+` FROM notifications WHERE `+where+`
		ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'acknowledged' THEN 1 ELSE 2 END,
		         CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,
		         updated_at DESC
		LIMIT $`+strconv.Itoa(i)+` OFFSET $`+strconv.Itoa(i+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Notification
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *n)
	}
	return out, total, rows.Err()
}

func (r *Repository) CountUnread(ctx context.Context, recipientUserID uuid.UUID) (int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var n int
	err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications
		WHERE recipient_user_id=$1 AND status='open'`, recipientUserID).Scan(&n)
	return n, err
}

func (r *Repository) ListOpenOlderThan(ctx context.Context, olderThan time.Time, limit int) ([]domain.Notification, error) {
	if limit <= 0 {
		limit = 200
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+cols+` FROM notifications
		WHERE status='open' AND created_at < $1
		ORDER BY created_at ASC LIMIT $2`, olderThan, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Notification
	for rows.Next() {
		n, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (r *Repository) GetPreference(ctx context.Context, userID uuid.UUID) (*domain.Preference, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var p domain.Preference
	err := q.QueryRow(ctx, `
		SELECT user_id, email_enabled, push_enabled, updated_at
		FROM notification_preferences WHERE user_id=$1`, userID).
		Scan(&p.UserID, &p.EmailEnabled, &p.PushEnabled, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *Repository) UpsertPreference(ctx context.Context, p *domain.Preference) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO notification_preferences (user_id, email_enabled, push_enabled, updated_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id) DO UPDATE SET
			email_enabled=EXCLUDED.email_enabled,
			push_enabled=EXCLUDED.push_enabled,
			updated_at=EXCLUDED.updated_at`,
		p.UserID, p.EmailEnabled, p.PushEnabled, p.UpdatedAt,
	)
	return err
}
