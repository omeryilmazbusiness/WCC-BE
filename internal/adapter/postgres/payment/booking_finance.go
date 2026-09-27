package payment

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var _ domain.BookingFinance = (*Repository)(nil)

func (r *Repository) Components(ctx context.Context, bookingID uuid.UUID) (domain.Components, error) {
	scope, args, err := bookingVisible(ctx, "li.booking_id", []any{bookingID})
	if err != nil {
		return domain.Components{}, err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var c domain.Components
	err = q.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(li.quantity::bigint * li.unit_price) FILTER (WHERE li.kind = 'item'), 0),
			COALESCE(SUM(li.quantity::bigint * li.unit_price) FILTER (WHERE li.kind = 'tax'), 0),
			COALESCE(SUM(li.quantity::bigint * li.unit_price) FILTER (WHERE li.kind = 'fee'), 0),
			COUNT(*) > 0
		FROM booking_line_items li
		WHERE li.booking_id = $1`+scope, args...).Scan(&c.Items, &c.Tax, &c.Fees, &c.HasLines)
	return c, err
}

func (r *Repository) ReportingSnapshot(ctx context.Context, bookingID uuid.UUID) (*domain.ReportingSnapshot, error) {
	scope, args, err := bookingVisible(ctx, "bk.id", []any{bookingID})
	if err != nil {
		return nil, err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	var amount, rate *int64
	var currency *string
	var date *time.Time
	err = q.QueryRow(ctx, `
		SELECT bk.total_reporting, bk.reporting_currency, bk.fx_rate_scaled, bk.fx_effective_date
		FROM bookings bk WHERE bk.id = $1`+scope, args...).Scan(&amount, &currency, &rate, &date)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("booking")
	}
	if err != nil {
		return nil, err
	}
	return snapshotFrom(amount, currency, rate, date), nil
}

// SetReportingSnapshot writes only the snapshot columns; the booking module
// owns every other bookings column.
func (r *Repository) SetReportingSnapshot(ctx context.Context, bookingID uuid.UUID, s domain.ReportingSnapshot) error {
	scope, args, err := bookingVisible(ctx, "bookings.id", []any{bookingID, s.Amount, s.Currency, s.RateScaled, s.EffectiveDate})
	if err != nil {
		return err
	}
	q := tx.QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `
		UPDATE bookings SET total_reporting=$2, reporting_currency=$3, fx_rate_scaled=$4, fx_effective_date=$5
		WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	return execOne(tag, "booking")
}
