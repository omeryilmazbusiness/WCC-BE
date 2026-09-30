package revenuetarget_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/revenuetarget"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type reportingCur string

func (r reportingCur) GetFinanceSettings(context.Context, uuid.UUID) (string, error) {
	return string(r), nil
}

// fakeFX converts USD at 3.75 and knows no other pair.
type fakeFX struct{}

func (fakeFX) Convert(_ context.Context, amount int64, from, to string, _ time.Time) (fxdomain.Conversion, error) {
	if from == "USD" && to == "SAR" {
		return fxdomain.Conversion{Amount: amount * 375 / 100}, nil
	}
	return fxdomain.Conversion{}, fxdomain.ErrRateNotFound
}

func isValidation(err error) bool {
	return errors.Is(err, shared.ErrValidation)
}

func TestCreateResolvesCalendarPeriodAndReportingCurrency(t *testing.T) {
	svc := appsvc.NewService(newMemRepo(), nil)
	svc.SetMoneySources(reportingCur("usd"), fakeFX{})
	tg, err := svc.Create(context.Background(), appsvc.CreateInput{
		BranchID: uuid.New(), Label: "September", TargetAmount: 1_000_00,
		PeriodKind: domain.PeriodMonthly, PeriodStart: time.Date(2026, 9, 17, 15, 0, 0, 0, time.UTC),
		ActorID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if tg.PeriodStart.Format(time.DateOnly) != "2026-09-01" || tg.PeriodEnd.Format(time.DateOnly) != "2026-09-30" {
		t.Fatalf("monthly range = %s..%s", tg.PeriodStart, tg.PeriodEnd)
	}
	if tg.Currency != "USD" || tg.PeriodKind != domain.PeriodMonthly {
		t.Fatalf("currency=%s kind=%s", tg.Currency, tg.PeriodKind)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	svc := appsvc.NewService(newMemRepo(), nil)
	base := appsvc.CreateInput{
		BranchID: uuid.New(), Label: "x", TargetAmount: 100,
		PeriodKind: domain.PeriodCustom, PeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
	}
	bad := map[string]func(*appsvc.CreateInput){
		"custom without end": func(in *appsvc.CreateInput) { in.PeriodEnd = time.Time{} },
		"unknown kind":       func(in *appsvc.CreateInput) { in.PeriodKind = "quarterly" },
		"bad currency":       func(in *appsvc.CreateInput) { in.Currency = "RIYAL" },
		"zero amount":        func(in *appsvc.CreateInput) { in.TargetAmount = 0 },
		"bad curve":          func(in *appsvc.CreateInput) { in.CurveType = "steep" },
	}
	for name, mutate := range bad {
		in := base
		mutate(&in)
		if _, err := svc.Create(context.Background(), in); !isValidation(err) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
	if _, err := svc.Create(context.Background(), base); err != nil {
		t.Fatalf("valid custom target: %v", err)
	}
}

func TestProgressConvertsForeignActuals(t *testing.T) {
	repo := newMemRepo()
	svc := appsvc.NewService(repo, nil)
	svc.SetMoneySources(reportingCur("SAR"), fakeFX{})
	today := time.Now().UTC()
	tg, err := svc.Create(context.Background(), appsvc.CreateInput{
		BranchID: uuid.New(), Label: "Year", TargetAmount: 1_000_000, Currency: "SAR",
		PeriodKind: domain.PeriodYearly, PeriodStart: today,
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.sums = []domain.CurrencyAmount{{Currency: "SAR", Minor: 1000}, {Currency: "USD", Minor: 400}, {Currency: "EUR", Minor: 50}}
	p, err := svc.Progress(context.Background(), tg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActualAmount != 1000+1500 {
		t.Fatalf("actual = %d, want 2500", p.ActualAmount)
	}
	if len(p.Unconverted) != 1 || p.Unconverted[0] != "EUR" {
		t.Fatalf("unconverted = %v", p.Unconverted)
	}
	if p.PeriodKind != domain.PeriodYearly || p.DaysLeft < 1 || p.DaysTotal < 365 {
		t.Fatalf("kind=%s daysLeft=%d daysTotal=%d", p.PeriodKind, p.DaysLeft, p.DaysTotal)
	}
}

func TestActualWithoutConverterReportsForeignCurrencies(t *testing.T) {
	repo := newMemRepo()
	svc := appsvc.NewService(repo, nil)
	tg, _ := svc.Create(context.Background(), appsvc.CreateInput{
		BranchID: uuid.New(), Label: "Week", TargetAmount: 100, Currency: "SAR",
		PeriodKind: domain.PeriodWeekly, PeriodStart: time.Now().UTC(),
	})
	repo.sums = []domain.CurrencyAmount{{Currency: "USD", Minor: 400}}
	p, err := svc.Progress(context.Background(), tg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.ActualAmount != 0 || len(p.Unconverted) != 1 {
		t.Fatalf("actual=%d unconverted=%v", p.ActualAmount, p.Unconverted)
	}
}

func TestActiveListsRunningBranchTargetsShortestFirst(t *testing.T) {
	repo := newMemRepo()
	svc := appsvc.NewService(repo, nil)
	branch := uuid.New()
	today := time.Now().UTC()
	mk := func(label string, kind domain.PeriodKind, start, end time.Time, owner *uuid.UUID) {
		t.Helper()
		if _, err := svc.Create(context.Background(), appsvc.CreateInput{
			BranchID: branch, OwnerID: owner, Label: label, TargetAmount: 1000, Currency: "SAR",
			PeriodKind: kind, PeriodStart: start, PeriodEnd: end,
		}); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	emp := uuid.New()
	mk("year", domain.PeriodYearly, today, time.Time{}, nil)
	mk("week", domain.PeriodWeekly, today, time.Time{}, nil)
	mk("month", domain.PeriodMonthly, today, time.Time{}, nil)
	mk("past", domain.PeriodCustom, today.AddDate(0, 0, -40), today.AddDate(0, 0, -10), nil)
	mk("future", domain.PeriodSeason, today.AddDate(0, 0, 5), today.AddDate(0, 0, 60), nil)
	mk("personal", domain.PeriodMonthly, today, time.Time{}, &emp)

	items, err := svc.Active(context.Background(), &branch)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(items))
	for _, p := range items {
		got = append(got, p.Label)
	}
	want := []string{"week", "month", "year"}
	if len(got) != len(want) {
		t.Fatalf("active = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("active = %v, want %v", got, want)
		}
	}
}

func TestUpdateReSnapsPeriodAndDeleteRemoves(t *testing.T) {
	svc := appsvc.NewService(newMemRepo(), nil)
	tg, err := svc.Create(context.Background(), appsvc.CreateInput{
		BranchID: uuid.New(), Label: "Q", TargetAmount: 100, Currency: "SAR",
		PeriodKind: domain.PeriodCustom, PeriodStart: time.Date(2026, 3, 3, 0, 0, 0, 0, time.UTC),
		PeriodEnd: time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	kind := domain.PeriodWeekly
	up, err := svc.Update(context.Background(), appsvc.PatchInput{ID: tg.ID, PeriodKind: &kind})
	if err != nil {
		t.Fatal(err)
	}
	if up.PeriodStart.Format(time.DateOnly) != "2026-03-02" || up.PeriodEnd.Format(time.DateOnly) != "2026-03-08" {
		t.Fatalf("weekly re-snap = %s..%s", up.PeriodStart, up.PeriodEnd)
	}
	if err := svc.Delete(context.Background(), tg.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(context.Background(), tg.ID); err == nil {
		t.Fatal("deleted target is still readable")
	}
	if err := svc.Delete(context.Background(), tg.ID, uuid.New()); err == nil {
		t.Fatal("second delete should be not found")
	}
}
