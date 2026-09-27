package payment

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Payments and schedules have no branch/owner of their own; they inherit
// visibility from their booking.
var scopeBooking = pgscope.Columns{Branch: "b.branch_id", Owner: "b.owner_id"}

// bookingVisible renders an EXISTS predicate restricting rows whose
// booking_id column is col to bookings in the caller's scope.
func bookingVisible(ctx context.Context, col string, args []any) (string, []any, error) {
	scope, args, err := pgscope.Clause(ctx, scopeBooking, args)
	if err != nil {
		return "", args, err
	}
	return " AND EXISTS (SELECT 1 FROM bookings b WHERE b.id = " + col + scope + ")", args, nil
}

// bookingFilter scopes a query that already joins bookings as b, optionally
// pinned to branchID.
func bookingFilter(ctx context.Context, branchID *uuid.UUID, args []any) (string, []any, error) {
	clause := ""
	if branchID != nil {
		args = append(args, *branchID)
		clause = fmt.Sprintf(" AND b.branch_id=$%d", len(args))
	}
	scope, args, err := pgscope.Clause(ctx, scopeBooking, args)
	return clause + scope, args, err
}

func (r *Repository) requireBooking(ctx context.Context, bookingID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "$1::uuid", []any{bookingID})
	if err != nil {
		return err
	}
	var ok bool
	if err := q.QueryRow(ctx, `SELECT TRUE`+scope, args...).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return shared.NewNotFound("booking")
	}
	return nil
}

func (r *Repository) scheduleRows(ctx context.Context, sql string, args ...any) ([]domain.Schedule, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, sql, args...)
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

func (r *Repository) paymentRows(ctx context.Context, sql string, args ...any) ([]domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, sql, args...)
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

func execOne(tag interface{ RowsAffected() int64 }, entity string) error {
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound(entity)
	}
	return nil
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const paymentCols = `p.id, p.booking_id, p.amount, p.currency, p.method, p.reference, p.recorded_by, p.idempotency_key,
	COALESCE(p.event_type,'charge'), p.reverses_payment_id, COALESCE(p.status,'verified'),
	p.approved_by, p.approved_at, COALESCE(p.note,''), p.received_at,
	p.amount_reporting, p.reporting_currency, p.fx_rate_scaled, p.fx_effective_date, p.created_at`

const scheduleCols = `s.id, s.booking_id, s.due_at, s.amount, s.currency, s.label, s.status, s.reminder_sent_at,
	s.created_at, s.updated_at`

func scanPayment(scan func(dest ...any) error) (*domain.Payment, error) {
	var p domain.Payment
	var eventType, status string
	var repAmount, repRate *int64
	var repCurrency *string
	var repDate *time.Time
	err := scan(
		&p.ID, &p.BookingID, &p.Amount, &p.Currency, &p.Method, &p.Reference, &p.RecordedBy, &p.IdempotencyKey,
		&eventType, &p.ReversesPaymentID, &status, &p.ApprovedBy, &p.ApprovedAt, &p.Note, &p.ReceivedAt,
		&repAmount, &repCurrency, &repRate, &repDate, &p.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.EventType = domain.EventType(eventType)
	p.Status = domain.Status(status)
	p.Reporting = snapshotFrom(repAmount, repCurrency, repRate, repDate)
	return &p, nil
}

func snapshotFrom(amount *int64, currency *string, rate *int64, date *time.Time) *domain.ReportingSnapshot {
	if amount == nil || currency == nil || rate == nil || date == nil {
		return nil
	}
	return &domain.ReportingSnapshot{Currency: *currency, Amount: *amount, RateScaled: *rate, EffectiveDate: *date}
}

// snapshotArgs splits a snapshot into nullable column values.
func snapshotArgs(s *domain.ReportingSnapshot) (amount *int64, currency *string, rate *int64, date *time.Time) {
	if s == nil {
		return nil, nil, nil, nil
	}
	return &s.Amount, &s.Currency, &s.RateScaled, &s.EffectiveDate
}

func (r *Repository) Insert(ctx context.Context, p *domain.Payment) error {
	if err := r.requireBooking(ctx, p.BookingID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	if p.EventType == "" {
		p.EventType = domain.EventCharge
	}
	if p.Status == "" {
		p.Status = domain.StatusUnverified
	}
	if p.ReceivedAt.IsZero() {
		p.ReceivedAt = p.CreatedAt
	}
	repAmount, repCurrency, repRate, repDate := snapshotArgs(p.Reporting)
	_, err := q.Exec(ctx, `
		INSERT INTO payments (
			id, booking_id, amount, currency, method, reference, recorded_by, idempotency_key,
			event_type, reverses_payment_id, status, approved_by, approved_at, note, received_at,
			amount_reporting, reporting_currency, fx_rate_scaled, fx_effective_date, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		p.ID, p.BookingID, p.Amount, p.Currency, p.Method, p.Reference, p.RecordedBy, p.IdempotencyKey,
		string(p.EventType), p.ReversesPaymentID, string(p.Status), p.ApprovedBy, p.ApprovedAt, p.Note,
		fxdomain.DateOf(p.ReceivedAt), repAmount, repCurrency, repRate, repDate, p.CreatedAt,
	)
	return err
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.Status, approvedBy *uuid.UUID, approvedAt *time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "payments.booking_id", []any{id, string(status), approvedBy, approvedAt})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `
		UPDATE payments SET status=$2, approved_by=$3, approved_at=$4 WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	return execOne(tag, "payment")
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "p.booking_id", []any{id})
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `SELECT `+paymentCols+` FROM payments p WHERE p.id=$1`+scope, args...)
	p, err := scanPayment(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *Repository) FindByIdempotencyKey(ctx context.Context, key string) (*domain.Payment, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "p.booking_id", []any{key})
	if err != nil {
		return nil, err
	}
	row := q.QueryRow(ctx, `SELECT `+paymentCols+` FROM payments p WHERE p.idempotency_key=$1`+scope, args...)
	p, err := scanPayment(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return p, err
}

func (r *Repository) SumCollectedByBooking(ctx context.Context, bookingID uuid.UUID) (int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "p.booking_id", []any{bookingID})
	if err != nil {
		return 0, err
	}
	var sum int64
	err = q.QueryRow(ctx, `
		SELECT COALESCE(SUM(p.amount),0) FROM payments p
		WHERE p.booking_id=$1 AND p.status IN ('verified','approved')`+scope, args...).Scan(&sum)
	return sum, err
}

func (r *Repository) SumByBookingStatus(ctx context.Context, bookingID uuid.UUID, status domain.Status) (int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "p.booking_id", []any{bookingID, string(status)})
	if err != nil {
		return 0, err
	}
	var sum int64
	err = q.QueryRow(ctx, `
		SELECT COALESCE(SUM(ABS(p.amount)),0) FROM payments p WHERE p.booking_id=$1 AND p.status=$2`+scope,
		args...).Scan(&sum)
	return sum, err
}

func (r *Repository) ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]domain.Payment, error) {
	scope, args, err := bookingVisible(ctx, "p.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	return r.paymentRows(ctx, `SELECT `+paymentCols+` FROM payments p WHERE p.booking_id=$1`+scope+` ORDER BY p.created_at`, args...)
}

func (r *Repository) ListByStatus(ctx context.Context, branchID *uuid.UUID, status domain.Status, eventType *domain.EventType, limit int) ([]domain.Payment, error) {
	if limit <= 0 {
		limit = 100
	}
	args := []any{string(status)}
	where := "p.status=$1"
	if eventType != nil {
		args = append(args, string(*eventType))
		where += fmt.Sprintf(" AND p.event_type=$%d", len(args))
	}
	scope, args, err := bookingFilter(ctx, branchID, args)
	if err != nil {
		return nil, err
	}
	args = append(args, limit)
	return r.paymentRows(ctx, fmt.Sprintf(`
		SELECT `+paymentCols+`
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id
		WHERE `+where+scope+`
		ORDER BY p.created_at DESC LIMIT $%d`, len(args)), args...)
}

func (r *Repository) UpsertSchedule(ctx context.Context, s *domain.Schedule) error {
	if err := r.requireBooking(ctx, s.BookingID); err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO payment_schedules (id, booking_id, due_at, amount, currency, label, status, reminder_sent_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET
			due_at=EXCLUDED.due_at, amount=EXCLUDED.amount, currency=EXCLUDED.currency,
			label=EXCLUDED.label, status=EXCLUDED.status, updated_at=EXCLUDED.updated_at
		WHERE payment_schedules.booking_id = EXCLUDED.booking_id`,
		s.ID, s.BookingID, s.DueAt, s.Amount, s.Currency, s.Label, string(s.Status), s.ReminderSentAt, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *Repository) ListSchedules(ctx context.Context, bookingID uuid.UUID) ([]domain.Schedule, error) {
	scope, args, err := bookingVisible(ctx, "s.booking_id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	return r.scheduleRows(ctx, `
		SELECT `+scheduleCols+`
		FROM payment_schedules s WHERE s.booking_id=$1`+scope+` ORDER BY s.due_at`, args...)
}

func (r *Repository) GetSchedule(ctx context.Context, id uuid.UUID) (*domain.Schedule, error) {
	scope, args, err := bookingVisible(ctx, "s.booking_id", []any{id})
	if err != nil {
		return nil, err
	}
	items, err := r.scheduleRows(ctx, `
		SELECT `+scheduleCols+`
		FROM payment_schedules s WHERE s.id=$1`+scope, args...)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return &items[0], nil
}

func (r *Repository) UpdateScheduleStatus(ctx context.Context, id uuid.UUID, status domain.ScheduleStatus) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "payment_schedules.booking_id", []any{id, string(status)})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `UPDATE payment_schedules SET status=$2, updated_at=NOW() WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	return execOne(tag, "schedule")
}

func (r *Repository) ListOverdueSchedules(ctx context.Context, branchID *uuid.UUID, now time.Time, limit int) ([]domain.Schedule, error) {
	if limit <= 0 {
		limit = 100
	}
	scope, args, err := bookingFilter(ctx, branchID, []any{now})
	if err != nil {
		return nil, err
	}
	args = append(args, limit)
	return r.scheduleRows(ctx, fmt.Sprintf(`
		SELECT `+scheduleCols+`
		FROM payment_schedules s
		JOIN bookings b ON b.id = s.booking_id
		WHERE s.status IN ('open','overdue') AND s.due_at < $1`+scope+`
		ORDER BY s.due_at LIMIT $%d`, len(args)), args...)
}

func (r *Repository) MarkSchedulesOverdue(ctx context.Context, now time.Time, limit int) ([]domain.Schedule, error) {
	if limit <= 0 {
		limit = 100
	}
	scope, args, err := bookingFilter(ctx, nil, []any{now, limit})
	if err != nil {
		return nil, err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		UPDATE payment_schedules s SET status='overdue', updated_at=NOW()
		FROM bookings b
		WHERE b.id = s.booking_id AND s.status='open' AND s.id IN (
			SELECT id FROM payment_schedules
			WHERE status='open' AND due_at < $1
			ORDER BY due_at, id LIMIT $2
			FOR UPDATE SKIP LOCKED
		)`+scope+`
		RETURNING `+scheduleCols+`, b.branch_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Schedule
	for rows.Next() {
		var s domain.Schedule
		var st string
		if err := rows.Scan(&s.ID, &s.BookingID, &s.DueAt, &s.Amount, &s.Currency, &s.Label, &st, &s.ReminderSentAt,
			&s.CreatedAt, &s.UpdatedAt, &s.BranchID); err != nil {
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
	scope, args, err := bookingFilter(ctx, nil, []any{now.Add(within), now.Add(-24 * time.Hour)})
	if err != nil {
		return nil, err
	}
	args = append(args, limit)
	return r.scheduleRows(ctx, fmt.Sprintf(`
		SELECT `+scheduleCols+`
		FROM payment_schedules s
		JOIN bookings b ON b.id = s.booking_id
		WHERE s.status='open' AND s.reminder_sent_at IS NULL AND s.due_at <= $1 AND s.due_at >= $2`+scope+`
		ORDER BY s.due_at LIMIT $%d`, len(args)), args...)
}

func (r *Repository) MarkReminderSent(ctx context.Context, id uuid.UUID, at time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	scope, args, err := bookingVisible(ctx, "payment_schedules.booking_id", []any{id, at})
	if err != nil {
		return err
	}
	tag, err := q.Exec(ctx, `UPDATE payment_schedules SET reminder_sent_at=$2, updated_at=NOW() WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	return execOne(tag, "schedule")
}

func (r *Repository) ListCreditBookings(ctx context.Context, branchID *uuid.UUID, limit int) ([]domain.QueueItem, error) {
	if limit <= 0 {
		limit = 100
	}
	scope, args, err := bookingFilter(ctx, branchID, nil)
	if err != nil {
		return nil, err
	}
	args = append(args, limit)
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, fmt.Sprintf(`
		SELECT b.id, LEFT(b.id::text, 8), COALESCE(c.full_name,''),
			(b.collected_amt - b.total_amount) AS credit, b.currency
		FROM bookings b
		LEFT JOIN customers c ON c.id = b.customer_id
		WHERE b.collected_amt > b.total_amount`+scope+`
		ORDER BY credit DESC LIMIT $%d`, len(args)), args...)
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
