package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const paymentCols = `id, booking_id, amount, currency, method, reference, recorded_by, idempotency_key,
	COALESCE(event_type,'charge'), reverses_payment_id, COALESCE(status,'verified'),
	approved_by, approved_at, COALESCE(note,''), created_at`

func scanPayment(scan func(dest ...any) error) (*domain.Payment, error) {
	var p domain.Payment
	var eventType, status string
	err := scan(
		&p.ID, &p.BookingID, &p.Amount, &p.Currency, &p.Method, &p.Reference, &p.RecordedBy, &p.IdempotencyKey,
		&eventType, &p.ReversesPaymentID, &status, &p.ApprovedBy, &p.ApprovedAt, &p.Note, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.EventType = domain.EventType(eventType)
	p.Status = domain.Status(status)
	return &p, nil
}

func (r *Repository) Insert(ctx context.Context, p *domain.Payment) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if p.EventType == "" {
		p.EventType = domain.EventCharge
	}
	if p.Status == "" {
		p.Status = domain.StatusUnverified
	}
	_, err := q.Exec(ctx, `
		INSERT INTO payments (
			id, booking_id, amount, currency, method, reference, recorded_by, idempotency_key,
			event_type, reverses_payment_id, status, approved_by, approved_at, note, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		p.ID, p.BookingID, p.Amount, p.Currency, p.Method, p.Reference, p.RecordedBy, p.IdempotencyKey,
		string(p.EventType), p.ReversesPaymentID, string(p.Status), p.ApprovedBy, p.ApprovedAt, p.Note, p.CreatedAt,
	)
	return err
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.Status, approvedBy *uuid.UUID, approvedAt *time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE payments SET status=$2, approved_by=$3, approved_at=$4 WHERE id=$1`,
		id, string(status), approvedBy, approvedAt)
	return err
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+paymentCols+` FROM payments WHERE id=$1`, id)
	p, err := scanPayment(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+paymentCols+` FROM payments WHERE idempotency_key=$1`, key)
	p, err := scanPayment(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return p, err
}

func (r *Repository) SumCollectedByBooking(ctx context.Context, bookingID uuid.UUID) (int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var sum int64
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(amount),0) FROM payments
		WHERE booking_id=$1 AND status IN ('verified','approved')`, bookingID).Scan(&sum)
	return sum, err
}

func (r *Repository) SumByBookingStatus(ctx context.Context, bookingID uuid.UUID, status domain.Status) (int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var sum int64
	err := q.QueryRow(ctx, `
		SELECT COALESCE(SUM(ABS(amount)),0) FROM payments WHERE booking_id=$1 AND status=$2`,
		bookingID, string(status)).Scan(&sum)
	return sum, err
}

func (r *Repository) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `SELECT `+paymentCols+` FROM payments WHERE booking_id=$1 ORDER BY created_at`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Payment
	for rows.Next() {
		p, err := scanPayment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *Repository) ListByStatus(ctx context.Context, branchID uuid.UUID, status domain.Status, eventType *domain.EventType, limit int) ([]domain.Payment, error) {
	if limit <= 0 {
		limit = 100
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var rows pgx.Rows
	var err error
	if eventType != nil {
		rows, err = q.Query(ctx, `
			SELECT p.id, p.booking_id, p.amount, p.currency, p.method, p.reference, p.recorded_by, p.idempotency_key,
				COALESCE(p.event_type,'charge'), p.reverses_payment_id, COALESCE(p.status,'verified'),
				p.approved_by, p.approved_at, COALESCE(p.note,''), p.created_at
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.branch_id=$1 AND p.status=$2 AND p.event_type=$3
			ORDER BY p.created_at DESC LIMIT $4`, branchID, string(status), string(*eventType), limit)
	} else {
		rows, err = q.Query(ctx, `
			SELECT p.id, p.booking_id, p.amount, p.currency, p.method, p.reference, p.recorded_by, p.idempotency_key,
				COALESCE(p.event_type,'charge'), p.reverses_payment_id, COALESCE(p.status,'verified'),
				p.approved_by, p.approved_at, COALESCE(p.note,''), p.created_at
			FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.branch_id=$1 AND p.status=$2
			ORDER BY p.created_at DESC LIMIT $3`, branchID, string(status), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Payment
	for rows.Next() {
		p, err := scanPayment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertSchedule(ctx context.Context, s *domain.Schedule) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO payment_schedules (id, booking_id, due_at, amount, currency, label, status, reminder_sent_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
			due_at=EXCLUDED.due_at, amount=EXCLUDED.amount, currency=EXCLUDED.currency,
			label=EXCLUDED.label, status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`,
		s.ID, s.BookingID, s.DueAt, s.Amount, s.Currency, s.Label, string(s.Status), s.ReminderSentAt, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *Repository) ListSchedules(ctx context.Context, bookingID uuid.UUID) ([]domain.Schedule, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, booking_id, due_at, amount, currency, label, status, reminder_sent_at, created_at, updated_at
		FROM payment_schedules WHERE booking_id=$1 ORDER BY due_at`, bookingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		var s domain.Schedule
		var st string
		if err := rows.Scan(&s.ID, &s.BookingID, &s.DueAt, &s.Amount, &s.Currency, &s.Label, &st, &s.ReminderSentAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Status = domain.ScheduleStatus(st)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) GetSchedule(ctx context.Context, id uuid.UUID) (*domain.Schedule, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, booking_id, due_at, amount, currency, label, status, reminder_sent_at, created_at, updated_at
		FROM payment_schedules WHERE id=$1`, id)
	var s domain.Schedule
	var st string
	err := row.Scan(&s.ID, &s.BookingID, &s.DueAt, &s.Amount, &s.Currency, &s.Label, &st, &s.ReminderSentAt, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Status = domain.ScheduleStatus(st)
	return &s, nil
}

func (r *Repository) UpdateScheduleStatus(ctx context.Context, id uuid.UUID, status domain.ScheduleStatus) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE payment_schedules SET status=$2, updated_at=NOW() WHERE id=$1`, id, string(status))
	return err
}

func (r *Repository) ListOverdueSchedules(ctx context.Context, branchID uuid.UUID, now time.Time, limit int) ([]domain.Schedule, error) {
	if limit <= 0 {
		limit = 100
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT s.id, s.booking_id, s.due_at, s.amount, s.currency, s.label, s.status, s.reminder_sent_at, s.created_at, s.updated_at
		FROM payment_schedules s
		JOIN bookings b ON b.id = s.booking_id
		WHERE b.branch_id=$1 AND s.status IN ('open','overdue') AND s.due_at < $2
		ORDER BY s.due_at LIMIT $3`, branchID, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		var s domain.Schedule
		var st string
		if err := rows.Scan(&s.ID, &s.BookingID, &s.DueAt, &s.Amount, &s.Currency, &s.Label, &st, &s.ReminderSentAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Status = domain.ScheduleStatus(st)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) ListDueForReminder(ctx context.Context, now time.Time, within time.Duration, limit int) ([]domain.Schedule, error) {
	if limit <= 0 {
		limit = 100
	}
	until := now.Add(within)
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, booking_id, due_at, amount, currency, label, status, reminder_sent_at, created_at, updated_at
		FROM payment_schedules
		WHERE status='open' AND reminder_sent_at IS NULL AND due_at <= $1 AND due_at >= $2
		ORDER BY due_at LIMIT $3`, until, now.Add(-24*time.Hour), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		var s domain.Schedule
		var st string
		if err := rows.Scan(&s.ID, &s.BookingID, &s.DueAt, &s.Amount, &s.Currency, &s.Label, &st, &s.ReminderSentAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.Status = domain.ScheduleStatus(st)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) MarkReminderSent(ctx context.Context, id uuid.UUID, at time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `UPDATE payment_schedules SET reminder_sent_at=$2, updated_at=NOW() WHERE id=$1`, id, at)
	return err
}

func (r *Repository) ListCreditBookings(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.QueueItem, error) {
	if limit <= 0 {
		limit = 100
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT b.id, LEFT(b.id::text, 8), COALESCE(c.full_name,''),
			(b.collected_amt - b.total_amount) AS credit, b.currency
		FROM bookings b
		LEFT JOIN customers c ON c.id = b.customer_id
		WHERE b.branch_id=$1 AND b.collected_amt > b.total_amount
		ORDER BY credit DESC LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.QueueItem
	for rows.Next() {
		var item domain.QueueItem
		if err := rows.Scan(&item.BookingID, &item.BookingRef, &item.CustomerName, &item.Amount, &item.Currency); err != nil {
			return nil, err
		}
		item.Kind = domain.QueueCredit
		item.Status = "credit"
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Repository) GetFinanceSettings(ctx context.Context, branchID uuid.UUID) (string, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var cur string
	err := q.QueryRow(ctx, `SELECT reporting_currency FROM finance_settings WHERE branch_id=$1`, branchID).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return "SAR", nil
	}
	return cur, err
}

func (r *Repository) UpsertFinanceSettings(ctx context.Context, branchID uuid.UUID, reportingCurrency string) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO finance_settings (branch_id, reporting_currency, updated_at)
		VALUES ($1,$2,NOW())
		ON CONFLICT (branch_id) DO UPDATE SET reporting_currency=EXCLUDED.reporting_currency, updated_at=NOW()`,
		branchID, reportingCurrency)
	return err
}
