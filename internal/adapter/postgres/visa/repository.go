package visa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/visa"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, v *domain.VisaCase) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO visa_cases (
			id, branch_id, booking_id, participant_id, customer_id, status, external_ref, notes,
			submitted_at, decided_at, expires_at, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		v.ID, v.BranchID, v.BookingID, v.ParticipantID, v.CustomerID, string(v.Status), v.ExternalRef, v.Notes,
		v.SubmittedAt, v.DecidedAt, v.ExpiresAt, v.CreatedBy, v.CreatedAt, v.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, v *domain.VisaCase) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE visa_cases SET
			participant_id=$2, customer_id=$3, status=$4, external_ref=$5, notes=$6,
			submitted_at=$7, decided_at=$8, expires_at=$9, updated_at=$10
		WHERE id=$1`,
		v.ID, v.ParticipantID, v.CustomerID, string(v.Status), v.ExternalRef, v.Notes,
		v.SubmittedAt, v.DecidedAt, v.ExpiresAt, v.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func scanCase(row pgx.Row) (*domain.VisaCase, error) {
	var v domain.VisaCase
	var status string
	var expiresAt *time.Time
	err := row.Scan(
		&v.ID, &v.BranchID, &v.BookingID, &v.ParticipantID, &v.CustomerID, &status, &v.ExternalRef, &v.Notes,
		&v.SubmittedAt, &v.DecidedAt, &expiresAt, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	v.Status = domain.Status(status)
	v.ExpiresAt = expiresAt
	return &v, nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.VisaCase, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	return scanCase(q.QueryRow(ctx, `
		SELECT id, branch_id, booking_id, participant_id, customer_id, status, external_ref, notes,
			submitted_at, decided_at, expires_at, created_by, created_at, updated_at
		FROM visa_cases WHERE id=$1`, id))
}

func (r *Repository) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.VisaCase, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, booking_id, participant_id, customer_id, status, external_ref, notes,
			submitted_at, decided_at, expires_at, created_by, created_at, updated_at
		FROM visa_cases WHERE booking_id=$1 ORDER BY created_at DESC`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.VisaCase
	for rows.Next() {
		var v domain.VisaCase
		var status string
		var expiresAt *time.Time
		if err := rows.Scan(
			&v.ID, &v.BranchID, &v.BookingID, &v.ParticipantID, &v.CustomerID, &status, &v.ExternalRef, &v.Notes,
			&v.SubmittedAt, &v.DecidedAt, &expiresAt, &v.CreatedBy, &v.CreatedAt, &v.UpdatedAt,
		); err != nil {
			return nil, err
		}
		v.Status = domain.Status(status)
		v.ExpiresAt = expiresAt
		out = append(out, v)
	}
	return out, rows.Err()
}

func (r *Repository) AppendEvent(ctx context.Context, e *domain.Event) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO visa_events (id, visa_case_id, from_status, to_status, actor_id, note, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ID, e.VisaCaseID, string(e.FromStatus), string(e.ToStatus), e.ActorID, e.Note, e.CreatedAt,
	)
	return err
}

func (r *Repository) ListEvents(ctx context.Context, visaCaseID uuid.UUID) ([]domain.Event, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, visa_case_id, from_status, to_status, actor_id, note, created_at
		FROM visa_events WHERE visa_case_id=$1 ORDER BY created_at ASC`, visaCaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var from, to string
		if err := rows.Scan(&e.ID, &e.VisaCaseID, &from, &to, &e.ActorID, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.FromStatus = domain.Status(from)
		e.ToStatus = domain.Status(to)
		out = append(out, e)
	}
	return out, rows.Err()
}
