package assistant

import "time"

// Mode tells whether model answers are still available today.
type Mode string

const (
	ModeFull      Mode = "full"      // model answers available
	ModeEssential Mode = "essential" // answers from data and rules only
)

// Quota is a user's model-answer allowance for one day. A non-positive Limit
// means unlimited.
type Quota struct {
	Limit    int
	Used     int
	ResetsAt time.Time
}

func (q Quota) Exhausted() bool { return q.Limit > 0 && q.Used >= q.Limit }

// Remaining is -1 when unlimited.
func (q Quota) Remaining() int {
	if q.Limit <= 0 {
		return -1
	}
	return max(0, q.Limit-q.Used)
}

func (q Quota) Mode() Mode {
	if q.Exhausted() {
		return ModeEssential
	}
	return ModeFull
}

// Day is the start of the calendar day of `now` in loc; the quota bucket.
func Day(now time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	t := now.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// NextReset is the next midnight after `now` in loc.
func NextReset(now time.Time, loc *time.Location) time.Time {
	return Day(now, loc).AddDate(0, 0, 1)
}
