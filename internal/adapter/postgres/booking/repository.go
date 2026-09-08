package booking

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, b *domain.Booking) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO bookings (
			id, branch_id, customer_id, departure_id, lead_id, status, pax_count,
			total_amount, collected_amt, balance_amt, currency, owner_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		b.ID, b.BranchID, b.CustomerID, b.DepartureID, b.LeadID, b.Status, b.PaxCount,
		b.TotalAmount, b.CollectedAmt, b.BalanceAmt, b.Currency, b.OwnerID, b.CreatedAt, b.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, b *domain.Booking) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE bookings SET status=$2, pax_count=$3, total_amount=$4, collected_amt=$5, balance_amt=$6,
			currency=$7, owner_id=$8, updated_at=$9
		WHERE id=$1`,
		b.ID, b.Status, b.PaxCount, b.TotalAmount, b.CollectedAmt, b.BalanceAmt, b.Currency, b.OwnerID, b.UpdatedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, customer_id, departure_id, lead_id, status, pax_count,
			total_amount, collected_amt, balance_amt, currency, owner_id, created_at, updated_at
		FROM bookings WHERE id=$1`, id)
	var b domain.Booking
	var status string
	err := row.Scan(&b.ID, &b.BranchID, &b.CustomerID, &b.DepartureID, &b.LeadID, &status, &b.PaxCount,
		&b.TotalAmount, &b.CollectedAmt, &b.BalanceAmt, &b.Currency, &b.OwnerID, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	b.Status = domain.Status(status)
	return &b, nil
}

func (r *Repository) AddParticipant(ctx context.Context, p *domain.Participant) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO booking_participants (id, booking_id, full_name, passport_no, nationality, date_of_birth, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		p.ID, p.BookingID, p.FullName, p.PassportNo, p.Nationality, p.DateOfBirth, p.CreatedAt,
	)
	return err
}

func (r *Repository) ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]domain.Participant, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, booking_id, full_name, passport_no, nationality, date_of_birth, created_at
		FROM booking_participants WHERE booking_id=$1`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Participant
	for rows.Next() {
		var p domain.Participant
		if err := rows.Scan(&p.ID, &p.BookingID, &p.FullName, &p.PassportNo, &p.Nationality, &p.DateOfBirth, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) CountConfirmedPaxByDeparture(ctx context.Context, departureID uuid.UUID) (int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var n int
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(pax_count),0) FROM bookings
		WHERE departure_id=$1 AND status='confirmed'`, departureID).Scan(&n)
	return n, err
}
