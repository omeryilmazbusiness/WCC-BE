package rooming

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/rooming"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListRooms(ctx context.Context, departureID uuid.UUID) ([]domain.Room, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT r.id, r.departure_id, r.branch_id, r.label, r.room_type, r.capacity, r.notes, r.sort_order, r.created_at,
			(SELECT COUNT(*)::int FROM room_assignments a WHERE a.room_id=r.id) AS assigned
		FROM departure_rooms r
		WHERE r.departure_id=$1
		ORDER BY r.sort_order, r.created_at`, departureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Room
	for rows.Next() {
		var room domain.Room
		var roomType string
		if err := rows.Scan(
			&room.ID, &room.DepartureID, &room.BranchID, &room.Label, &roomType,
			&room.Capacity, &room.Notes, &room.SortOrder, &room.CreatedAt, &room.Assigned,
		); err != nil {
			return nil, err
		}
		room.RoomType = domain.RoomType(roomType)
		out = append(out, room)
	}
	return out, rows.Err()
}

func (r *Repository) CreateRoom(ctx context.Context, room *domain.Room) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO departure_rooms (
			id, departure_id, branch_id, label, room_type, capacity, notes, sort_order, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		room.ID, room.DepartureID, room.BranchID, room.Label, string(room.RoomType),
		room.Capacity, room.Notes, room.SortOrder, room.CreatedAt,
	)
	return err
}

func (r *Repository) UpdateRoom(ctx context.Context, room *domain.Room) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE departure_rooms SET
			label=$2, room_type=$3, capacity=$4, notes=$5, sort_order=$6
		WHERE id=$1`,
		room.ID, room.Label, string(room.RoomType), room.Capacity, room.Notes, room.SortOrder,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) DeleteRoom(ctx context.Context, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `DELETE FROM departure_rooms WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) Assign(ctx context.Context, a *domain.Assignment) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO room_assignments (id, room_id, participant_id, booking_id, assigned_at, assigned_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (participant_id) DO UPDATE SET
			room_id = EXCLUDED.room_id,
			booking_id = EXCLUDED.booking_id,
			assigned_at = EXCLUDED.assigned_at,
			assigned_by = EXCLUDED.assigned_by`,
		a.ID, a.RoomID, a.ParticipantID, a.BookingID, a.AssignedAt, a.AssignedBy,
	)
	return err
}

func (r *Repository) Unassign(ctx context.Context, participantID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `DELETE FROM room_assignments WHERE participant_id=$1`, participantID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) ListAssignments(ctx context.Context, departureID uuid.UUID) ([]domain.Assignment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT a.id, a.room_id, a.participant_id, a.booking_id, a.assigned_at, a.assigned_by,
			COALESCE(p.full_name,''), COALESCE(p.passport_no,'')
		FROM room_assignments a
		JOIN departure_rooms r ON r.id = a.room_id
		JOIN booking_participants p ON p.id = a.participant_id
		WHERE r.departure_id=$1
		ORDER BY a.assigned_at`, departureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Assignment
	for rows.Next() {
		var a domain.Assignment
		if err := rows.Scan(
			&a.ID, &a.RoomID, &a.ParticipantID, &a.BookingID, &a.AssignedAt, &a.AssignedBy,
			&a.ParticipantName, &a.PassportNo,
		); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) GroupList(ctx context.Context, departureID uuid.UUID) ([]domain.GroupListRow, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT p.id, b.id, p.full_name, COALESCE(p.passport_no,''), COALESCE(p.nationality,''),
			a.room_id, COALESCE(r.label,''), (a.room_id IS NULL) AS unassigned
		FROM booking_participants p
		JOIN bookings b ON b.id = p.booking_id
		LEFT JOIN room_assignments a ON a.participant_id = p.id
		LEFT JOIN departure_rooms r ON r.id = a.room_id
		WHERE b.departure_id=$1 AND b.status <> 'cancelled'
		ORDER BY unassigned DESC, COALESCE(r.sort_order, 9999), p.full_name`, departureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.GroupListRow
	for rows.Next() {
		var row domain.GroupListRow
		if err := rows.Scan(
			&row.ParticipantID, &row.BookingID, &row.FullName, &row.PassportNo, &row.Nationality,
			&row.RoomID, &row.RoomLabel, &row.Unassigned,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
