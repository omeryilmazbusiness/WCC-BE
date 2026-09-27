package report

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/report"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type schedMem struct {
	schedules map[uuid.UUID]domain.Schedule
	runs      map[string]domain.ScheduledRun
}

func newSchedMem() *schedMem {
	return &schedMem{schedules: map[uuid.UUID]domain.Schedule{}, runs: map[string]domain.ScheduledRun{}}
}

func (m *schedMem) ListSchedules(_ context.Context, branchID uuid.UUID) ([]domain.Schedule, error) {
	var out []domain.Schedule
	for _, s := range m.schedules {
		if s.BranchID == branchID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (m *schedMem) GetSchedule(_ context.Context, id uuid.UUID) (*domain.Schedule, error) {
	s := m.schedules[id]
	return &s, nil
}
func (m *schedMem) InsertSchedule(_ context.Context, s *domain.Schedule) error {
	m.schedules[s.ID] = *s
	return nil
}
func (m *schedMem) UpdateSchedule(_ context.Context, s *domain.Schedule) error {
	m.schedules[s.ID] = *s
	return nil
}
func (m *schedMem) DeleteSchedule(_ context.Context, id uuid.UUID) error {
	delete(m.schedules, id)
	return nil
}
func (m *schedMem) ListEnabledSchedules(context.Context, int) ([]domain.Schedule, error) {
	var out []domain.Schedule
	for _, s := range m.schedules {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out, nil
}
func (m *schedMem) RunExists(_ context.Context, id uuid.UUID, period string) (bool, error) {
	_, ok := m.runs[id.String()+period]
	return ok, nil
}
func (m *schedMem) InsertRun(_ context.Context, r *domain.ScheduledRun) (bool, error) {
	k := r.ScheduleID.String() + r.Period
	if _, ok := m.runs[k]; ok {
		return false, nil
	}
	m.runs[k] = *r
	return true, nil
}
func (m *schedMem) MarkScheduleRun(context.Context, uuid.UUID, time.Time) error { return nil }
func (m *schedMem) ListRuns(context.Context, uuid.UUID, *uuid.UUID, int) ([]domain.ScheduledRun, error) {
	return nil, nil
}
func (m *schedMem) GetRun(context.Context, uuid.UUID) (*domain.ScheduledRun, error) { return nil, nil }

type genFake struct{ filters []domain.Filter }

func (g *genFake) Run(ctx context.Context, kind domain.Kind, f domain.Filter) (*domain.Result, error) {
	if s := access.From(ctx); s.BranchID != f.BranchID {
		panic("report generated outside branch scope")
	}
	g.filters = append(g.filters, f)
	return &domain.Result{Kind: kind, Columns: []string{"label"}, Rows: []domain.Row{{Label: "x"}}}, nil
}

type eligibleFake map[uuid.UUID]bool

func (e eligibleFake) Eligible(context.Context, uuid.UUID) (map[uuid.UUID]bool, error) { return e, nil }

type readyFake struct{ calls [][]uuid.UUID }

func (r *readyFake) ReportReady(_ context.Context, _ domain.ScheduledRun, to []uuid.UUID) error {
	r.calls = append(r.calls, to)
	return nil
}

func TestFrequencyPeriod(t *testing.T) {
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC) // Wednesday
	cases := []struct {
		f        domain.Frequency
		key      string
		from, to string
	}{
		{domain.FrequencyDaily, "2026-09-15", "2026-09-15", "2026-09-16"},
		{domain.FrequencyWeekly, "2026-W37", "2026-09-07", "2026-09-14"},
		{domain.FrequencyMonthly, "2026-08", "2026-08-01", "2026-09-01"},
	}
	for _, c := range cases {
		key, from, to := c.f.Period(now, time.UTC)
		if key != c.key || from.Format("2006-01-02") != c.from || to.Format("2006-01-02") != c.to {
			t.Fatalf("%s: got %s %s %s", c.f, key, from, to)
		}
	}
}

func TestSchedulerRejectsIneligibleRecipient(t *testing.T) {
	branch, ok, stranger := uuid.New(), uuid.New(), uuid.New()
	s := NewScheduler(newSchedMem(), &genFake{}, eligibleFake{ok: true}, nil, tx.Nop{}, time.UTC)
	if _, err := s.Create(context.Background(), branch, ScheduleInput{
		Kind: domain.KindSales, Frequency: domain.FrequencyDaily, RecipientIDs: []uuid.UUID{stranger},
	}); err == nil {
		t.Fatal("expected validation error for ineligible recipient")
	}
	if _, err := s.Create(context.Background(), branch, ScheduleInput{
		Kind: domain.KindIntegrations, Frequency: domain.FrequencyDaily, RecipientIDs: []uuid.UUID{ok},
	}); err == nil {
		t.Fatal("integrations reports cannot be scheduled")
	}
}

func TestSchedulerRunDueOncePerPeriod(t *testing.T) {
	branch, keep, gone := uuid.New(), uuid.New(), uuid.New()
	repo := newSchedMem()
	eligible := eligibleFake{keep: true, gone: true}
	gen, ready := &genFake{}, &readyFake{}
	s := NewScheduler(repo, gen, eligible, ready, tx.Nop{}, time.UTC)
	s.now = func() time.Time { return time.Date(2026, 9, 16, 8, 0, 0, 0, time.UTC) }
	if _, err := s.Create(context.Background(), branch, ScheduleInput{
		Kind: domain.KindSales, Frequency: domain.FrequencyWeekly, RecipientIDs: []uuid.UUID{keep, gone, keep},
	}); err != nil {
		t.Fatal(err)
	}
	delete(eligible, gone)

	for i := 0; i < 2; i++ {
		n, err := s.RunDue(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if want := 1 - i; n != want {
			t.Fatalf("run %d: generated %d, want %d", i, n, want)
		}
	}
	if len(gen.filters) != 1 || len(ready.calls) != 1 {
		t.Fatalf("generated %d, notified %d", len(gen.filters), len(ready.calls))
	}
	if got := ready.calls[0]; len(got) != 1 || got[0] != keep {
		t.Fatalf("recipients = %v", got)
	}
	if f := gen.filters[0]; f.From.Format("2006-01-02") != "2026-09-07" || f.To.Format("2006-01-02") != "2026-09-14" {
		t.Fatalf("filter period = %s..%s", f.From, f.To)
	}
}
