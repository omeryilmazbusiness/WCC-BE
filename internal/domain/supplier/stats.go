package supplier

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// MetricsWindowDays is the look-back of performance figures.
const MetricsWindowDays = 30

const maxDailyCount = 10_000_000

// Usage is a batch of API traffic counters reported for one day.
type Usage struct {
	SupplierID   uuid.UUID
	Day          time.Time
	Searches     int
	Bookings     int
	Errors       int
	PriceChanges int
	SoldOuts     int
	// LatencyMs is the average latency of the batch; 0 when not measured.
	LatencyMs int
}

// Validate checks a usage report; the day may not be in the future or older
// than the metrics window.
func (u *Usage) Validate(today time.Time) error {
	f := fields{}
	for key, v := range map[string]int{
		"searches": u.Searches, "bookings": u.Bookings, "errors": u.Errors,
		"price_changes": u.PriceChanges, "sold_outs": u.SoldOuts, "latency_ms": u.LatencyMs,
	} {
		if v < 0 || v > maxDailyCount {
			f.add(key, "0-10000000")
		}
	}
	if u.Day.IsZero() {
		u.Day = today
	}
	if u.Day.After(today) || u.Day.Before(today.AddDate(0, 0, -MetricsWindowDays)) {
		f.add("day", "within the last 30 days")
	}
	if u.Searches+u.Bookings+u.Errors+u.PriceChanges+u.SoldOuts == 0 {
		f.add("searches", "report at least one counter")
	}
	return f.err("invalid usage report")
}

// Metrics are aggregated API figures over the window.
type Metrics struct {
	Searches     int64
	Bookings     int64
	Errors       int64
	PriceChanges int64
	SoldOuts     int64
	AvgLatencyMs int
}

// BookingsPer1000 is the look-to-book figure: bookings per 1000 searches.
func (m Metrics) BookingsPer1000() float64 {
	if m.Searches == 0 {
		return 0
	}
	return round1(float64(m.Bookings) * 1000 / float64(m.Searches))
}

// ErrorRatePct is the share of searches that failed.
func (m Metrics) ErrorRatePct() float64 {
	if m.Searches == 0 {
		return 0
	}
	return round1(float64(m.Errors) * 100 / float64(m.Searches))
}

// FailedBookingPct is the share of booking attempts lost to "price changed"
// or "sold out" answers.
func (m Metrics) FailedBookingPct() float64 {
	attempts := m.Bookings + m.PriceChanges + m.SoldOuts
	if attempts == 0 {
		return 0
	}
	return round1(float64(m.PriceChanges+m.SoldOuts) * 100 / float64(attempts))
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

// Volume is business done with the supplier over the window, from the ledger.
type Volume struct {
	Spend    int64
	Bookings int
	Refunds  int64
}

// Dispute statuses.
const (
	DisputeOpen     = "open"
	DisputeResolved = "resolved"
	DisputeRejected = "rejected"
)

const (
	MaxDisputeTitle      = 200
	MaxDisputeResolution = 2000
	MaxBookingRef        = 60
)

// Dispute is a customer-impacting case being settled with the supplier.
type Dispute struct {
	ID         uuid.UUID
	SupplierID uuid.UUID
	BranchID   uuid.UUID
	Title      string
	BookingRef string
	Amount     int64
	Currency   string
	Status     string
	Resolution string
	OpenedBy   *uuid.UUID
	OpenedAt   time.Time
	ResolvedAt *time.Time
	UpdatedAt  time.Time
}

func (d *Dispute) Normalize() error {
	f := fields{}
	d.Title = strings.TrimSpace(d.Title)
	d.BookingRef = strings.TrimSpace(d.BookingRef)
	d.Resolution = strings.TrimSpace(d.Resolution)
	if n := len([]rune(d.Title)); n < 3 || n > MaxDisputeTitle {
		f.add("title", "3-200 characters")
	}
	if len([]rune(d.BookingRef)) > MaxBookingRef {
		f.add("booking_ref", "at most 60 characters")
	}
	if len([]rune(d.Resolution)) > MaxDisputeResolution {
		f.add("resolution", "at most 2000 characters")
	}
	if d.Amount < 0 || d.Amount > MaxMoney {
		f.add("amount", "out of range")
	}
	if !currencyPattern.MatchString(d.Currency) {
		f.add("currency", "ISO 4217 code")
	}
	return f.err("invalid dispute")
}

// Close resolves or rejects an open dispute.
func (d *Dispute) Close(status, resolution string, at time.Time) error {
	if d.Status != DisputeOpen {
		return shared.NewInvalidState("dispute is already closed")
	}
	if status != DisputeResolved && status != DisputeRejected {
		e := shared.NewValidation("status must be resolved or rejected")
		e.Details = map[string]any{"status": "resolved or rejected"}
		return e
	}
	d.Status, d.Resolution, d.ResolvedAt, d.UpdatedAt = status, resolution, &at, at
	return d.Normalize()
}
