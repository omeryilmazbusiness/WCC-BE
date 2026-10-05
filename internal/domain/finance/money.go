// Package finance is the finance hub: treasury (cash, bank and POS
// accounts), B2B agency credit risk and receivable ageing, per-booking
// profitability and sales commission, tax-aware invoicing, refund
// settlement, BSP reconciliation and balance confirmation letters.
//
// Amounts are int64 minor units of the stated ISO 4217 currency.
package finance

import (
	"regexp"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// MaxMoney bounds every amount (10 billion major units at 2 decimals).
const MaxMoney int64 = 1_000_000_000_000

// BPS is the basis-point scale: 10000 = 100 %.
const BPS = 10_000

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

// NormalizeCurrency upper-cases and validates an ISO 4217 code.
func NormalizeCurrency(c string) (string, bool) {
	c = strings.ToUpper(strings.TrimSpace(c))
	return c, currencyPattern.MatchString(c)
}

// fields collects field-level validation messages.
type fields map[string]any

func (f fields) add(k, msg string) { f[k] = msg }

func (f fields) err(msg string) error {
	if len(f) == 0 {
		return nil
	}
	e := shared.NewValidation(msg)
	e.Details = map[string]any(f)
	return e
}

func fieldErr(field, msg string) error {
	e := shared.NewValidation(msg)
	e.Details = map[string]any{field: msg}
	return e
}

func inRange(v int64) bool { return v > 0 && v <= MaxMoney }

// ApplyBPS returns amount * bps / 10000 rounded half away from zero.
func ApplyBPS(amount int64, bps int) int64 {
	p := amount * int64(bps)
	if p >= 0 {
		return (p + BPS/2) / BPS
	}
	return -((-p + BPS/2) / BPS)
}

// RatioBPS is part/whole in basis points, rounded half away from zero (0
// when whole is 0).
func RatioBPS(part, whole int64) int {
	if whole == 0 {
		return 0
	}
	num, den := part*BPS, whole
	if (num < 0) != (den < 0) {
		return int(-((abs(num) + abs(den)/2) / abs(den)))
	}
	return int((abs(num) + abs(den)/2) / abs(den))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// Day truncates t to its calendar date in loc, returned as UTC midnight.
func Day(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DaysBetween counts whole calendar days from a to b (negative when b < a).
func DaysBetween(a, b time.Time) int {
	return int(Day(b, time.UTC).Sub(Day(a, time.UTC)).Hours() / 24)
}

// ParseDay parses YYYY-MM-DD.
func ParseDay(v string) (time.Time, error) {
	return time.Parse(time.DateOnly, strings.TrimSpace(v))
}

// MonthBounds returns [first day of t's month, first day of next month).
func MonthBounds(t time.Time) (time.Time, time.Time) {
	y, m, _ := t.Date()
	from := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	return from, from.AddDate(0, 1, 0)
}
