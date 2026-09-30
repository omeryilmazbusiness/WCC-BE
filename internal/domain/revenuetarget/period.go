package revenuetarget

import (
	"errors"
	"time"
)

// PeriodKind is how a target's date range is chosen.
type PeriodKind string

const (
	PeriodWeekly  PeriodKind = "weekly"  // ISO week, Monday to Sunday
	PeriodMonthly PeriodKind = "monthly" // calendar month
	PeriodSeason  PeriodKind = "season"  // named selling season, explicit dates
	PeriodYearly  PeriodKind = "yearly"  // calendar year
	PeriodCustom  PeriodKind = "custom"  // any explicit range
)

// PeriodKinds lists kinds in display order (shortest first).
var PeriodKinds = []PeriodKind{PeriodWeekly, PeriodMonthly, PeriodSeason, PeriodYearly, PeriodCustom}

// MaxPeriodDays caps explicit ranges so pacing and seasonal weights stay meaningful.
const MaxPeriodDays = 3 * 366

var (
	ErrPeriodKind  = errors.New("period_kind must be weekly, monthly, season, yearly or custom")
	ErrPeriodStart = errors.New("period_start is required")
	ErrPeriodEnd   = errors.New("period_end must be on or after period_start")
	ErrPeriodSpan  = errors.New("period is longer than 3 years")
)

func (k PeriodKind) Valid() bool {
	for _, v := range PeriodKinds {
		if k == v {
			return true
		}
	}
	return false
}

// Calendar kinds derive their whole range from any date inside it.
func (k PeriodKind) Calendar() bool {
	return k == PeriodWeekly || k == PeriodMonthly || k == PeriodYearly
}

// Rank orders kinds from shortest to longest horizon.
func (k PeriodKind) Rank() int {
	for i, v := range PeriodKinds {
		if k == v {
			return i
		}
	}
	return len(PeriodKinds)
}

// ResolvePeriod returns the canonical [start, end] dates (UTC midnight) for kind.
// Calendar kinds snap to the week, month or year containing start and ignore end;
// season and custom require an explicit end.
func ResolvePeriod(kind PeriodKind, start, end time.Time) (time.Time, time.Time, error) {
	if !kind.Valid() {
		return time.Time{}, time.Time{}, ErrPeriodKind
	}
	if start.IsZero() {
		return time.Time{}, time.Time{}, ErrPeriodStart
	}
	s := dateOnly(start)
	switch kind {
	case PeriodWeekly:
		monday := s.AddDate(0, 0, -((int(s.Weekday()) + 6) % 7))
		return monday, monday.AddDate(0, 0, 6), nil
	case PeriodMonthly:
		first := time.Date(s.Year(), s.Month(), 1, 0, 0, 0, 0, time.UTC)
		return first, first.AddDate(0, 1, -1), nil
	case PeriodYearly:
		return time.Date(s.Year(), 1, 1, 0, 0, 0, 0, time.UTC), time.Date(s.Year(), 12, 31, 0, 0, 0, 0, time.UTC), nil
	}
	if end.IsZero() {
		return time.Time{}, time.Time{}, ErrPeriodEnd
	}
	e := dateOnly(end)
	if e.Before(s) {
		return time.Time{}, time.Time{}, ErrPeriodEnd
	}
	if DaysInclusive(s, e) > MaxPeriodDays {
		return time.Time{}, time.Time{}, ErrPeriodSpan
	}
	return s, e, nil
}

// DaysInclusive counts calendar days from start to end, both included.
func DaysInclusive(start, end time.Time) int {
	return int(dateOnly(end).Sub(dateOnly(start)).Hours()/24) + 1
}

// Covers reports whether day falls inside [start, end].
func Covers(start, end, day time.Time) bool {
	d := dateOnly(day)
	return !d.Before(dateOnly(start)) && !d.After(dateOnly(end))
}

func dateOnly(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
