package company

import "time"

// PlanTermYears is the fixed commercial length of every WCC package.
const PlanTermYears = 1

// ExpiringWithin is how long before the end date a term is "expiring".
const ExpiringWithin = 30 * 24 * time.Hour

type TermStatus string

const (
	TermActive   TermStatus = "active"
	TermExpiring TermStatus = "expiring"
	TermExpired  TermStatus = "expired"
)

// Term is the current 1-year window of a company's package.
type Term struct {
	Plan      Plan
	StartedAt time.Time
	EndsAt    time.Time
	DaysLeft  int
	Status    TermStatus
	Years     int
}

// StartOfTerm is when this package year began. Zero falls back to created_at.
func (c Company) StartOfTerm() time.Time {
	if !c.PlanStartedAt.IsZero() {
		return c.PlanStartedAt.UTC()
	}
	return c.CreatedAt.UTC()
}

func EndOfTerm(started time.Time) time.Time {
	s := started.UTC()
	return time.Date(s.Year()+PlanTermYears, s.Month(), s.Day(), s.Hour(), s.Minute(), s.Second(), s.Nanosecond(), time.UTC)
}

func daysLeft(now, end time.Time) int {
	n := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	e := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
	return int(e.Sub(n).Hours() / 24)
}

// TermAt describes the package year at `now`.
func (c Company) TermAt(now time.Time) Term {
	start := c.StartOfTerm()
	end := EndOfTerm(start)
	now = now.UTC()
	left := daysLeft(now, end)
	status := TermActive
	switch {
	case !now.Before(end):
		status = TermExpired
	case end.Sub(now) <= ExpiringWithin:
		status = TermExpiring
	}
	return Term{
		Plan: Of(c.Plan).Plan, StartedAt: start, EndsAt: end,
		DaysLeft: left, Status: status, Years: PlanTermYears,
	}
}
