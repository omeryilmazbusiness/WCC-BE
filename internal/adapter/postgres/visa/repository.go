package visa

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/visa"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// caseScope: visa cases inherit visibility from their booking (branch, and
// the booking owner for own/team scopes).
var caseScope = pgscope.Columns{
	Branch: "v.branch_id",
	Owner:  "(SELECT b.owner_id FROM bookings b WHERE b.id=v.booking_id)",
}

const caseCols = `v.id, v.branch_id, v.booking_id, v.participant_id, v.customer_id, v.status, v.external_ref, v.notes,
	v.submitted_at, v.decided_at, v.expires_at, v.created_by, v.created_at, v.updated_at`

// Create pins the case to its booking's branch and requires the booking to
// be visible to the caller.
func (r *Repository) Create(ctx context.Context, v *domain.VisaCase) error {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "b.branch_id", Owner: "b.owner_id"}, []any{v.BookingID})
	if err != nil {
		return err
	}
	var branchID uuid.UUID
	err = q.QueryRow(ctx, `SELECT b.branch_id FROM bookings b WHERE b.id=$1`+clause, args...).Scan(&branchID)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewNotFound("booking")
	}
	if err != nil {
		return err
	}
	v.BranchID = branchID
	_, err = q.Exec(ctx, `
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
	clause, args, err := pgscope.Clause(ctx, caseScope, []any{
		v.ID, v.ParticipantID, v.CustomerID, string(v.Status), v.ExternalRef, v.Notes,
		v.SubmittedAt, v.DecidedAt, v.ExpiresAt, v.UpdatedAt,
	})
	if err != nil {
		return err
	}
	ct, err := q.Exec(ctx, `
		UPDATE visa_cases v SET
			participant_id=$2, customer_id=$3, status=$4, external_ref=$5, notes=$6,
			submitted_at=$7, decided_at=$8, expires_at=$9, updated_at=$10
		WHERE v.id=$1`+clause, args...)
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
	clause, args, err := pgscope.Clause(ctx, caseScope, []any{id})
	if err != nil {
		return nil, err
	}
	return scanCase(q.QueryRow(ctx, `SELECT `+caseCols+` FROM visa_cases v WHERE v.id=$1`+clause, args...))
}

func (r *Repository) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.VisaCase, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := pgscope.Clause(ctx, caseScope, []any{bookingID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+caseCols+` FROM visa_cases v
		WHERE v.booking_id=$1`+clause+` ORDER BY v.created_at DESC`, args...)
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
	clause, args, err := pgscope.Clause(ctx, caseScope, []any{visaCaseID})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `
		SELECT e.id, e.visa_case_id, e.from_status, e.to_status, e.actor_id, e.note, e.created_at
		FROM visa_events e
		WHERE e.visa_case_id=$1
		  AND EXISTS (SELECT 1 FROM visa_cases v WHERE v.id=e.visa_case_id`+clause+`)
		ORDER BY e.created_at ASC`, args...)
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

// ListAwaitingDecision returns submitted/processing cases older than their
// branch's visa_follow_up_days (defaultDays without a settings row).
func (r *Repository) ListAwaitingDecision(ctx context.Context, now time.Time, defaultDays, limit int) ([]domain.FollowUpCandidate, error) {
	if limit <= 0 {
		limit = 200
	}
	q := tx.QuerierFrom(ctx, r.pool)
	clause, args, err := pgscope.Clause(ctx, caseScope, []any{now, defaultDays, limit})
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, `SELECT `+caseCols+`, b.owner_id FROM visa_cases v
		JOIN bookings b ON b.id = v.booking_id
		LEFT JOIN alert_threshold_settings ats ON ats.branch_id = v.branch_id
		WHERE v.status IN ('submitted','processing') AND v.submitted_at IS NOT NULL
		  AND v.submitted_at + make_interval(days => COALESCE(ats.visa_follow_up_days, $2)) <= $1`+clause+`
		ORDER BY v.submitted_at ASC LIMIT $3`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FollowUpCandidate
	for rows.Next() {
		var c domain.FollowUpCandidate
		var status string
		if err := rows.Scan(
			&c.Case.ID, &c.Case.BranchID, &c.Case.BookingID, &c.Case.ParticipantID, &c.Case.CustomerID, &status,
			&c.Case.ExternalRef, &c.Case.Notes, &c.Case.SubmittedAt, &c.Case.DecidedAt, &c.Case.ExpiresAt,
			&c.Case.CreatedBy, &c.Case.CreatedAt, &c.Case.UpdatedAt, &c.OwnerID,
		); err != nil {
			return nil, err
		}
		c.Case.Status = domain.Status(status)
		out = append(out, c)
	}
	return out, rows.Err()
}
