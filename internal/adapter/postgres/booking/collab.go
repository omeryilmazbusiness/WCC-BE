package booking

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var _ domain.CollabStore = (*Repository)(nil)

// UpdateProfile writes only the profile columns.
func (r *Repository) UpdateProfile(ctx context.Context, b *domain.Booking) error {
	return r.scopedExec(ctx, "booking", `
		UPDATE bookings SET pnr=$2, service_type=$3, supplier_source=$4, channel=$5, summary=$6, company_name=$7, updated_at=$8
		WHERE id=$1`, b.ID, b.PNR, b.ServiceType, b.SupplierSource, b.Channel, b.Summary, b.CompanyName, b.UpdatedAt)
}

// SaveHold moves the option deadline of a booking that is still on option.
func (r *Repository) SaveHold(ctx context.Context, b *domain.Booking) error {
	return r.scopedExec(ctx, "booking", `
		UPDATE bookings SET hold_expires_at=$2, updated_at=$3
		WHERE id=$1 AND status='option_hold'`, b.ID, b.HoldExpiresAt, b.UpdatedAt)
}

func (r *Repository) IncrementReissue(ctx context.Context, bookingID uuid.UUID) error {
	return r.scopedExec(ctx, "booking", `
		UPDATE bookings SET reissue_count = reissue_count + 1, updated_at = NOW() WHERE id=$1`, bookingID)
}

func pgscopeClause(ctx context.Context, args []any) (string, []any, error) {
	return pgscope.Clause(ctx, scopeBookings, args)
}

func (r *Repository) scopedExec(ctx context.Context, entity, sql string, args ...any) error {
	scope, args, err := pgscopeClause(ctx, args)
	if err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, sql+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound(entity)
	}
	return nil
}

func (r *Repository) CreateNote(ctx context.Context, n *domain.Note) error {
	if err := r.requireVisible(ctx, n.BookingID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO booking_notes (id, booking_id, author_id, body, pinned, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		n.ID, n.BookingID, n.AuthorID, n.Body, n.Pinned, n.CreatedAt)
	return err
}

const noteCols = `n.id, n.booking_id, n.author_id, COALESCE(u.full_name, ''), n.body, n.pinned, n.created_at`

func scanNote(row pgx.Row) (*domain.Note, error) {
	var n domain.Note
	err := row.Scan(&n.ID, &n.BookingID, &n.AuthorID, &n.AuthorName, &n.Body, &n.Pinned, &n.CreatedAt)
	return &n, err
}

func (r *Repository) FindNote(ctx context.Context, bookingID, noteID uuid.UUID) (*domain.Note, error) {
	scope, args, err := parentVisible(ctx, "n.booking_id", []any{noteID, bookingID})
	if err != nil {
		return nil, err
	}
	n, err := scanNote(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `SELECT `+noteCols+`
		FROM booking_notes n LEFT JOIN users u ON u.id = n.author_id
		WHERE n.id=$1 AND n.booking_id=$2`+scope, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("note")
	}
	return n, err
}

func (r *Repository) DeleteNote(ctx context.Context, bookingID, noteID uuid.UUID) error {
	scope, args, err := parentVisible(ctx, "booking_notes.booking_id", []any{noteID, bookingID})
	if err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `DELETE FROM booking_notes WHERE id=$1 AND booking_id=$2`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("note")
	}
	return nil
}

func (r *Repository) ListNotes(ctx context.Context, bookingID uuid.UUID) ([]domain.Note, error) {
	scope, args, err := parentVisible(ctx, "n.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `SELECT `+noteCols+`
		FROM booking_notes n LEFT JOIN users u ON u.id = n.author_id
		WHERE n.booking_id=$1`+scope+` ORDER BY n.pinned DESC, n.created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Note{}
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

func (r *Repository) CreateChange(ctx context.Context, c *domain.ChangeRequest) error {
	if err := r.requireVisible(ctx, c.BookingID); err != nil {
		return err
	}
	return tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `
		INSERT INTO booking_change_requests (id, booking_id, kind, details, status, requested_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING COALESCE((SELECT full_name FROM users WHERE id = $6), '')`,
		c.ID, c.BookingID, c.Kind, c.Details, c.Status, c.RequestedBy, c.CreatedAt).Scan(&c.RequestedByName)
}

const changeCols = `c.id, c.booking_id, c.kind, c.details, c.status, c.requested_by, COALESCE(u.full_name, ''),
	c.resolved_by, c.resolution_note, c.created_at, c.resolved_at`

func scanChange(row pgx.Row) (*domain.ChangeRequest, error) {
	var c domain.ChangeRequest
	err := row.Scan(&c.ID, &c.BookingID, &c.Kind, &c.Details, &c.Status, &c.RequestedBy, &c.RequestedByName,
		&c.ResolvedBy, &c.ResolutionNote, &c.CreatedAt, &c.ResolvedAt)
	return &c, err
}

func (r *Repository) FindChangeForUpdate(ctx context.Context, bookingID, changeID uuid.UUID) (*domain.ChangeRequest, error) {
	scope, args, err := parentVisible(ctx, "c.booking_id", []any{changeID, bookingID})
	if err != nil {
		return nil, err
	}
	c, err := scanChange(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `SELECT `+changeCols+`
		FROM booking_change_requests c LEFT JOIN users u ON u.id = c.requested_by
		WHERE c.id=$1 AND c.booking_id=$2`+scope+` FOR UPDATE OF c`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("change request")
	}
	return c, err
}

func (r *Repository) ResolveChange(ctx context.Context, c *domain.ChangeRequest) error {
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE booking_change_requests SET status=$3, resolved_by=$4, resolution_note=$5, resolved_at=$6
		WHERE id=$1 AND booking_id=$2 AND status='requested'`,
		c.ID, c.BookingID, c.Status, c.ResolvedBy, c.ResolutionNote, c.ResolvedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewConflict("change request was already resolved")
	}
	return nil
}

func (r *Repository) ListChanges(ctx context.Context, bookingID uuid.UUID) ([]domain.ChangeRequest, error) {
	scope, args, err := parentVisible(ctx, "c.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `SELECT `+changeCols+`
		FROM booking_change_requests c LEFT JOIN users u ON u.id = c.requested_by
		WHERE c.booking_id=$1`+scope+` ORDER BY c.created_at DESC LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ChangeRequest{}
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// ListActivity returns audit events about the booking and its children
// (participants, payments, notes) with payloads reduced to whitelisted keys.
func (r *Repository) ListActivity(ctx context.Context, bookingID uuid.UUID, limit int) ([]domain.Activity, error) {
	if err := r.requireVisible(ctx, bookingID); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT a.id, a.action, a.entity_type, COALESCE(u.full_name, ''), a.actor_type,
		       a.before, a.after, a.metadata, a.created_at
		FROM audit_events a LEFT JOIN users u ON u.id = a.actor_id
		WHERE (a.entity_type = 'booking' AND a.entity_id = $1)
		   OR (a.metadata ? 'booking_id' AND a.metadata->>'booking_id' = $2)
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $3`, bookingID, bookingID.String(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Activity{}
	for rows.Next() {
		var a domain.Activity
		var before, after, meta []byte
		if err := rows.Scan(&a.ID, &a.Action, &a.EntityType, &a.ActorName, &a.ActorType, &before, &after, &meta, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Details = activityDetails(before, after, meta)
		out = append(out, a)
	}
	return out, rows.Err()
}

func activityDetails(before, after, meta []byte) map[string]any {
	decode := func(raw []byte) map[string]any {
		m := map[string]any{}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &m)
		}
		return m
	}
	b, a, m := decode(before), decode(after), decode(meta)
	out := map[string]any{}
	for _, k := range domain.ActivityDetailKeys {
		if v, ok := a[k]; ok && v != nil {
			out[k] = v
		} else if v, ok := m[k]; ok && v != nil {
			out[k] = v
		}
		if v, ok := b[k]; ok && v != nil {
			if cur, has := out[k]; !has || !sameJSON(cur, v) {
				out["from_"+k] = v
			}
		}
	}
	return out
}

func sameJSON(x, y any) bool {
	a, _ := json.Marshal(x)
	b, _ := json.Marshal(y)
	return string(a) == string(b)
}
