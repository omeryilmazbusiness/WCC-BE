package revenuetarget

import (
	"errors"
	"testing"
	"time"
)

func day(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestResolvePeriodSnapsCalendarKinds(t *testing.T) {
	cases := []struct {
		kind       PeriodKind
		anchor     string
		start, end string
	}{
		{PeriodWeekly, "2026-09-30", "2026-09-28", "2026-10-04"}, // Wednesday
		{PeriodWeekly, "2026-09-28", "2026-09-28", "2026-10-04"}, // Monday
		{PeriodWeekly, "2026-10-04", "2026-09-28", "2026-10-04"}, // Sunday
		{PeriodWeekly, "2026-12-31", "2026-12-28", "2027-01-03"}, // crosses the year
		{PeriodMonthly, "2026-02-14", "2026-02-01", "2026-02-28"},
		{PeriodMonthly, "2028-02-10", "2028-02-01", "2028-02-29"}, // leap year
		{PeriodMonthly, "2026-12-31", "2026-12-01", "2026-12-31"},
		{PeriodYearly, "2026-06-15", "2026-01-01", "2026-12-31"},
	}
	for _, c := range cases {
		s, e, err := ResolvePeriod(c.kind, day(c.anchor), day("2030-01-01"))
		if err != nil {
			t.Fatalf("%s %s: %v", c.kind, c.anchor, err)
		}
		if s.Format(time.DateOnly) != c.start || e.Format(time.DateOnly) != c.end {
			t.Fatalf("%s %s: got %s..%s want %s..%s", c.kind, c.anchor, s.Format(time.DateOnly), e.Format(time.DateOnly), c.start, c.end)
		}
	}
}

func TestResolvePeriodExplicitRanges(t *testing.T) {
	s, e, err := ResolvePeriod(PeriodSeason, day("2027-04-01"), day("2027-06-30"))
	if err != nil || s != day("2027-04-01") || e != day("2027-06-30") {
		t.Fatalf("season: %v %v %v", s, e, err)
	}
	if _, _, err := ResolvePeriod(PeriodCustom, day("2027-04-01"), day("2027-04-01")); err != nil {
		t.Fatalf("one-day custom range: %v", err)
	}
	checks := []struct {
		name       string
		kind       PeriodKind
		start, end time.Time
		want       error
	}{
		{"unknown kind", "quarterly", day("2026-01-01"), day("2026-03-31"), ErrPeriodKind},
		{"no start", PeriodMonthly, time.Time{}, time.Time{}, ErrPeriodStart},
		{"custom without end", PeriodCustom, day("2026-01-01"), time.Time{}, ErrPeriodEnd},
		{"end before start", PeriodSeason, day("2026-05-01"), day("2026-04-30"), ErrPeriodEnd},
		{"too long", PeriodCustom, day("2026-01-01"), day("2029-06-01"), ErrPeriodSpan},
	}
	for _, c := range checks {
		if _, _, err := ResolvePeriod(c.kind, c.start, c.end); !errors.Is(err, c.want) {
			t.Fatalf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

func TestPeriodHelpers(t *testing.T) {
	if DaysInclusive(day("2026-09-28"), day("2026-10-04")) != 7 {
		t.Fatal("a week is 7 days")
	}
	if !Covers(day("2026-09-01"), day("2026-09-30"), day("2026-09-30").Add(23*time.Hour)) || Covers(day("2026-09-01"), day("2026-09-30"), day("2026-10-01")) {
		t.Fatal("covers must be inclusive by calendar day")
	}
	if PeriodWeekly.Rank() >= PeriodYearly.Rank() || PeriodSeason.Calendar() || !PeriodMonthly.Calendar() {
		t.Fatal("rank/calendar helpers")
	}
}
