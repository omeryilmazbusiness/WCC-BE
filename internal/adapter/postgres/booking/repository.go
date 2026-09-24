package booking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

const bookingCols = `id, branch_id, customer_id, departure_id, lead_id, status, pax_count,
	total_amount, discount_amt, cost_amt, collected_amt, balance_amt, currency, notes, owner_id, created_at, updated_at`

func scanBooking(row pgx.Row) (*domain.Booking, error) {
	var b domain.Booking
	var status string
	err := row.Scan(
		&b.ID, &b.BranchID, &b.CustomerID, &b.DepartureID, &b.LeadID, &status, &b.PaxCount,
		&b.TotalAmount, &b.DiscountAmt, &b.CostAmt, &b.CollectedAmt, &b.BalanceAmt, &b.Currency, &b.Notes,
		&b.OwnerID, &b.CreatedAt, &b.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	b.Status = domain.Status(status)
	return &b, nil
}

func (r *Repository) Create(ctx context.Context, b *domain.Booking) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO bookings (
			id, branch_id, customer_id, departure_id, lead_id, status, pax_count,
			total_amount, discount_amt, cost_amt, collected_amt, balance_amt, currency, notes, owner_id, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		b.ID, b.BranchID, b.CustomerID, b.DepartureID, b.LeadID, b.Status, b.PaxCount,
		b.TotalAmount, b.DiscountAmt, b.CostAmt, b.CollectedAmt, b.BalanceAmt, b.Currency, b.Notes,
		b.OwnerID, b.CreatedAt, b.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, b *domain.Booking) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE bookings SET status=$2, pax_count=$3, total_amount=$4, discount_amt=$5, cost_amt=$6,
			collected_amt=$7, balance_amt=$8, currency=$9, notes=$10, owner_id=$11, updated_at=$12
		WHERE id=$1`,
		b.ID, b.Status, b.PaxCount, b.TotalAmount, b.DiscountAmt, b.CostAmt,
		b.CollectedAmt, b.BalanceAmt, b.Currency, b.Notes, b.OwnerID, b.UpdatedAt,
	)
	return err
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	b, err := scanBooking(q.QueryRow(ctx, `SELECT `+bookingCols+` FROM bookings WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return b, err
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Booking, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	i := 1
	if f.BranchID != nil {
		where = append(where, fmt.Sprintf("branch_id=$%d", i))
		args = append(args, *f.BranchID)
		i++
	}
	if f.CustomerID != nil {
		where = append(where, fmt.Sprintf("customer_id=$%d", i))
		args = append(args, *f.CustomerID)
		i++
	}
	if f.DepartureID != nil {
		where = append(where, fmt.Sprintf("departure_id=$%d", i))
		args = append(args, *f.DepartureID)
		i++
	}
	if f.OwnerID != nil {
		where = append(where, fmt.Sprintf("owner_id=$%d", i))
		args = append(args, *f.OwnerID)
		i++
	}
	if f.LeadID != nil {
		where = append(where, fmt.Sprintf("lead_id=$%d", i))
		args = append(args, *f.LeadID)
		i++
	}
	if f.Status != "" {
		where = append(where, fmt.Sprintf("status=$%d", i))
		args = append(args, string(f.Status))
		i++
	}
	if qs := strings.TrimSpace(f.Query); qs != "" {
		where = append(where, fmt.Sprintf(`(notes ILIKE $%d OR CAST(id AS TEXT) ILIKE $%d)`, i, i))
		args = append(args, "%"+qs+"%")
		i++
	}
	clause := strings.Join(where, " AND ")
	limit, offset := f.Limit, f.Offset
	if limit <= 0 {
		limit = 50
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM bookings WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := fmt.Sprintf(`SELECT %s FROM bookings WHERE %s ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`,
		bookingCols, clause, i, i+1)
	args = append(args, limit, offset)
	rows, err := q.Query(ctx, listSQL, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *b)
	}
	return out, total, rows.Err()
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

func (r *Repository) UpdateParticipant(ctx context.Context, p *domain.Participant) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE booking_participants SET full_name=$3, passport_no=$4, nationality=$5, date_of_birth=$6
		WHERE id=$1 AND booking_id=$2`,
		p.ID, p.BookingID, p.FullName, p.PassportNo, p.Nationality, p.DateOfBirth,
	)
	return err
}

func (r *Repository) DeleteParticipant(ctx context.Context, bookingID, participantID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `DELETE FROM booking_participants WHERE id=$1 AND booking_id=$2`, participantID, bookingID)
	return err
}

func (r *Repository) ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]domain.Participant, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, booking_id, full_name, passport_no, nationality, date_of_birth, created_at
		FROM booking_participants WHERE booking_id=$1 ORDER BY created_at`, bookingID)
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

func (r *Repository) ListByDeparture(ctx context.Context, departureID uuid.UUID) ([]domain.Booking, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `SELECT `+bookingCols+` FROM bookings WHERE departure_id=$1 ORDER BY created_at DESC`, departureID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Booking
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceLineItems(ctx context.Context, bookingID uuid.UUID, items []domain.LineItem) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM booking_line_items WHERE booking_id=$1`, bookingID); err != nil {
		return err
	}
	now := time.Now().UTC()
	for i, it := range items {
		id := it.ID
		if id == uuid.Nil {
			id = uuid.New()
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO booking_line_items (
				id, booking_id, kind, label, quantity, unit_price, unit_cost, sort_order, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			id, bookingID, it.Kind, it.Label, it.Quantity, it.UnitPrice, it.UnitCost, i, now, now,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListLineItems(ctx context.Context, bookingID uuid.UUID) ([]domain.LineItem, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, booking_id, kind, label, quantity, unit_price, unit_cost, sort_order, created_at, updated_at
		FROM booking_line_items WHERE booking_id=$1 ORDER BY sort_order`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LineItem
	for rows.Next() {
		var l domain.LineItem
		if err := rows.Scan(&l.ID, &l.BookingID, &l.Kind, &l.Label, &l.Quantity, &l.UnitPrice, &l.UnitCost,
			&l.SortOrder, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Repository) SeedChecklist(ctx context.Context, items []domain.ChecklistItem) error {
	q := tx.QuerierFrom(ctx, r.pool)
	for _, it := range items {
		if _, err := q.Exec(ctx, `
			INSERT INTO booking_checklist_items (
				id, booking_id, code, label, required, completed, completed_at, sort_order, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (booking_id, code) DO NOTHING`,
			it.ID, it.BookingID, it.Code, it.Label, it.Required, it.Completed, it.CompletedAt, it.SortOrder, it.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListChecklist(ctx context.Context, bookingID uuid.UUID) ([]domain.ChecklistItem, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, booking_id, code, label, required, completed, completed_at, sort_order, created_at
		FROM booking_checklist_items WHERE booking_id=$1 ORDER BY sort_order`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ChecklistItem
	for rows.Next() {
		var c domain.ChecklistItem
		if err := rows.Scan(&c.ID, &c.BookingID, &c.Code, &c.Label, &c.Required, &c.Completed, &c.CompletedAt,
			&c.SortOrder, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repository) UpdateChecklistItem(ctx context.Context, item *domain.ChecklistItem) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE booking_checklist_items SET completed=$3, completed_at=$4, label=$5
		WHERE id=$1 AND booking_id=$2`,
		item.ID, item.BookingID, item.Completed, item.CompletedAt, item.Label,
	)
	return err
}

func (r *Repository) UpsertReadinessOverride(ctx context.Context, o *domain.ReadinessOverride) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO booking_readiness_overrides (id, booking_id, reason, actor_id, created_at)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (booking_id) DO UPDATE SET
			reason = EXCLUDED.reason,
			actor_id = EXCLUDED.actor_id,
			created_at = EXCLUDED.created_at,
			id = EXCLUDED.id`,
		o.ID, o.BookingID, o.Reason, o.ActorID, o.CreatedAt,
	)
	return err
}

func (r *Repository) FindReadinessOverride(ctx context.Context, bookingID uuid.UUID) (*domain.ReadinessOverride, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, booking_id, reason, actor_id, created_at
		FROM booking_readiness_overrides WHERE booking_id=$1`, bookingID)
	var o domain.ReadinessOverride
	err := row.Scan(&o.ID, &o.BookingID, &o.Reason, &o.ActorID, &o.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
