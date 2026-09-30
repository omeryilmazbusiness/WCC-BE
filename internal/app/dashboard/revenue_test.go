package dashboard_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
)

type stubRevenue struct {
	facts *dashboard.RevenueFacts
	got   dashboard.RevenueQuery
}

func (s *stubRevenue) RevenueFacts(_ context.Context, q dashboard.RevenueQuery) (*dashboard.RevenueFacts, error) {
	s.got = q
	return s.facts, nil
}

type stubCurrency string

func (c stubCurrency) GetFinanceSettings(context.Context, uuid.UUID) (string, error) {
	return string(c), nil
}

// stubFX converts USD→SAR at 3.75 and has no rate for anything else.
type stubFX struct{}

func (stubFX) Convert(_ context.Context, amount int64, from, to string, _ time.Time) (fxdomain.Conversion, error) {
	if from == "USD" && to == "SAR" {
		return fxdomain.Conversion{Amount: amount * 375 / 100}, nil
	}
	return fxdomain.Conversion{}, fxdomain.ErrRateNotFound
}

func sar(minor int64, n int) dashboard.Amount {
	return dashboard.Amount{Currency: "SAR", Minor: minor, Count: n}
}

func revenueService(t *testing.T, facts *dashboard.RevenueFacts) (*dashboard.Service, *stubRevenue, context.Context, uuid.UUID) {
	t.Helper()
	svc := dashboard.NewService(&stubAgg{kpi: &dashboard.KPI{}})
	rev := &stubRevenue{facts: facts}
	svc.SetRevenueSources(rev, stubCurrency("SAR"), stubFX{})
	branch := uuid.New()
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelBranch, BranchID: branch, UserID: uuid.New()})
	return svc, rev, ctx, branch
}

func TestRevenueConvertsToReportingCurrencyAndDerivesRates(t *testing.T) {
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -3)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	svc, rev, ctx, branch := revenueService(t, &dashboard.RevenueFacts{
		Booked:          []dashboard.Amount{sar(100_000, 2), {Currency: "USD", Minor: 40_000, Count: 1}},
		BookedCollected: []dashboard.Amount{sar(80_000, 0), {Currency: "USD", Minor: 20_000}},
		Margin:          []dashboard.Amount{sar(30_000, 1)},
		CostedBooked:    []dashboard.Amount{sar(60_000, 1)},
		Collected:       []dashboard.Amount{sar(90_000, 3)},
		Refunds:         []dashboard.Amount{sar(10_000, 1)},
		Outstanding:     []dashboard.Amount{sar(50_000, 2), {Currency: "EUR", Minor: 9_999, Count: 1}},
		Overdue:         []dashboard.Amount{sar(20_000, 1)},
		DueSoon:         []dashboard.Amount{{Currency: "USD", Minor: 10_000, Count: 1}},
		Pending:         []dashboard.Amount{sar(5_000, 1)},
		Methods: []dashboard.MethodAmounts{
			{Method: "cash", Amounts: []dashboard.Amount{sar(20_000, 1)}},
			{Method: "card", Amounts: []dashboard.Amount{sar(70_000, 2)}},
		},
		Daily: []dashboard.DayAmounts{{Day: today, Amounts: []dashboard.Amount{sar(80_000, 0)}}},
	})

	out, err := svc.Revenue(ctx, nil, from, now)
	if err != nil {
		t.Fatal(err)
	}
	if rev.got.BranchID != branch || rev.got.Currency != "SAR" {
		t.Fatalf("query: %+v", rev.got)
	}
	if out.Currency != "SAR" || out.Booked.Amount != 250_000 || out.Booked.Count != 3 {
		t.Fatalf("booked: %+v", out.Booked)
	}
	if out.NetCollected != 80_000 || out.Refunds.Amount != 10_000 {
		t.Fatalf("net collected: %d refunds %+v", out.NetCollected, out.Refunds)
	}
	// (80_000 + 75_000) / 250_000 = 62%; margin only over costed bookings: 30_000 / 60_000 = 50%.
	if out.CollectionPct == nil || *out.CollectionPct != 62 || out.MarginPct == nil || *out.MarginPct != 50 {
		t.Fatalf("rates: collection=%v margin=%v", out.CollectionPct, out.MarginPct)
	}
	if out.Margin == nil || *out.Margin != 30_000 || out.CostedBookings != 1 {
		t.Fatalf("margin: %v costed=%d", out.Margin, out.CostedBookings)
	}
	if out.Outstanding.Amount != 50_000 || out.Outstanding.Count != 3 {
		t.Fatalf("outstanding without EUR rate: %+v", out.Outstanding)
	}
	if len(out.Unconverted) != 1 || out.Unconverted[0] != "EUR" {
		t.Fatalf("unconverted: %v", out.Unconverted)
	}
	if out.DueSoon.Amount != 37_500 || out.Overdue.Amount != 20_000 || out.PendingVerification.Amount != 5_000 {
		t.Fatalf("schedules: due=%+v overdue=%+v pending=%+v", out.DueSoon, out.Overdue, out.PendingVerification)
	}
	if len(out.Methods) != 2 || out.Methods[0].Method != "card" {
		t.Fatalf("methods sorted by amount: %+v", out.Methods)
	}
	if len(out.Series) != 4 || out.Series[3].Amount != 80_000 || out.Series[0].Amount != 0 {
		t.Fatalf("series zero-filled per day: %+v", out.Series)
	}
}

func TestRevenueWithoutBookingsHasNoRates(t *testing.T) {
	svc, _, ctx, _ := revenueService(t, &dashboard.RevenueFacts{})
	now := time.Now().UTC()
	out, err := svc.Revenue(ctx, nil, now.AddDate(0, 0, -7), now)
	if err != nil {
		t.Fatal(err)
	}
	if out.CollectionPct != nil || out.MarginPct != nil || out.Margin != nil || len(out.Unconverted) != 0 || out.Methods == nil {
		t.Fatalf("empty revenue: %+v", out)
	}
}

func TestRevenueGlobalCallerMustPickABranch(t *testing.T) {
	svc, _, _, _ := revenueService(t, &dashboard.RevenueFacts{})
	now := time.Now().UTC()
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelGlobal, UserID: uuid.New()})
	if _, err := svc.Revenue(ctx, nil, now.AddDate(0, 0, -7), now); err == nil {
		t.Fatal("global caller without branch_id must be refused")
	}
	if _, err := svc.Revenue(context.Background(), nil, now.AddDate(0, 0, -7), now); !errors.Is(err, access.ErrNoScope) {
		t.Fatalf("want ErrNoScope, got %v", err)
	}
}
