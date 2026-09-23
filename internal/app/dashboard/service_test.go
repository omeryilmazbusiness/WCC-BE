package dashboard_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type stubAgg struct {
	lastFrom, lastTo time.Time
	branch           *uuid.UUID
	kpi              *dashboard.KPI
	err              error
	calls            int
}

func (s *stubAgg) Compute(_ context.Context, branchID *uuid.UUID, from, to time.Time) (*dashboard.KPI, error) {
	s.calls++
	s.branch = branchID
	s.lastFrom, s.lastTo = from, to
	if s.err != nil {
		return nil, s.err
	}
	out := *s.kpi
	return &out, nil
}
func (s *stubAgg) TeamPerformance(context.Context, *uuid.UUID, time.Time, time.Time) ([]dashboard.TeamMember, error) {
	return nil, nil
}
func (s *stubAgg) AttentionFeed(context.Context, *uuid.UUID, int) ([]dashboard.AttentionItem, error) {
	return nil, nil
}
func (s *stubAgg) MyWorkToday(context.Context, uuid.UUID, uuid.UUID, int) ([]dashboard.MyWorkItem, error) {
	return nil, nil
}
func (s *stubAgg) TargetProgress(context.Context, uuid.UUID, *uuid.UUID) (*dashboard.TargetProgress, error) {
	return &dashboard.TargetProgress{Status: "placeholder"}, nil
}

func TestTargetStatus(t *testing.T) {
	if dashboard.TargetStatus(110, 100) != "ahead" {
		t.Fatal("ahead")
	}
	if dashboard.TargetStatus(95, 100) != "on_track" {
		t.Fatal("on_track")
	}
	if dashboard.TargetStatus(50, 100) != "behind" {
		t.Fatal("behind")
	}
}

func TestNormalizePeriod(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	from, to, err := dashboard.NormalizePeriod(now.AddDate(0, 0, -7), now, now)
	if err != nil {
		t.Fatal(err)
	}
	if !from.Before(to) {
		t.Fatal("expected from < to")
	}

	_, _, err = dashboard.NormalizePeriod(now, now, now)
	if err == nil {
		t.Fatal("expected validation for equal bounds")
	}
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(app.Err, shared.ErrValidation) {
		t.Fatalf("want validation, got %v", err)
	}

	_, _, err = dashboard.NormalizePeriod(now.AddDate(-2, 0, 0), now, now)
	if err == nil {
		t.Fatal("expected max period validation")
	}
}

func TestDefaultPeriod(t *testing.T) {
	now := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	from, to := dashboard.DefaultPeriod(now)
	if !to.Equal(now) {
		t.Fatalf("to=%v want %v", to, now)
	}
	if want := now.AddDate(0, 0, -30); !from.Equal(want) {
		t.Fatalf("from=%v want %v", from, want)
	}
}

func TestServiceKPIsValidatesThenAggregates(t *testing.T) {
	now := time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)
	agg := &stubAgg{kpi: &dashboard.KPI{LeadsOpen: 3, TasksOverdue: 1}}
	svc := dashboard.NewService(agg)
	svcNow := now
	// inject clock via Normalize through KPIs with explicit window
	from := now.AddDate(0, 0, -14)
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	kpi, err := svc.KPIs(context.Background(), &branch, from, now)
	if err != nil {
		t.Fatal(err)
	}
	if agg.calls != 1 {
		t.Fatalf("calls=%d", agg.calls)
	}
	if agg.branch == nil || *agg.branch != branch {
		t.Fatal("branch not forwarded")
	}
	if !kpi.PeriodFrom.Equal(from.UTC()) || !kpi.PeriodTo.Equal(now.UTC()) {
		t.Fatalf("period overwritten incorrectly: %#v", kpi)
	}
	if kpi.LeadsOpen != 3 || kpi.TasksOverdue != 1 {
		t.Fatalf("unexpected kpi %#v", kpi)
	}
	_ = svcNow
}

func TestServiceKPIsRejectsInvalidPeriod(t *testing.T) {
	agg := &stubAgg{kpi: &dashboard.KPI{}}
	svc := dashboard.NewService(agg)
	now := time.Now().UTC()
	_, err := svc.KPIs(context.Background(), nil, now, now.Add(-time.Hour))
	if err == nil {
		t.Fatal("expected error")
	}
	if agg.calls != 0 {
		t.Fatal("aggregator must not run on invalid period")
	}
}
