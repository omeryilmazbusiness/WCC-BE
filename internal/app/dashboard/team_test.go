package dashboard_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func teamMembers() []dashboard.TeamMember {
	return []dashboard.TeamMember{
		{OwnerID: uuid.New(), OwnerName: "Aisha", Role: "employee", LeadsHandled: 5, LeadsWon: 2, Collected: []dashboard.Amount{
			{Currency: "SAR", Minor: 10_000, Count: 1},
			{Currency: "USD", Minor: 20_000, Count: 1},
			{Currency: "EUR", Minor: 5_000, Count: 1},
		}},
		{OwnerID: uuid.New(), OwnerName: "Omar", Role: "manager"},
	}
}

func teamWindow() (time.Time, time.Time) {
	to := time.Now().UTC()
	return to.AddDate(0, 0, -30), to
}

func TestTeamConvertsCollectedToReportingCurrency(t *testing.T) {
	svc := dashboard.NewService(&stubAgg{kpi: &dashboard.KPI{}, team: teamMembers()})
	svc.SetRevenueSources(&stubRevenue{}, stubCurrency("SAR"), stubFX{})
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelBranch, BranchID: uuid.New(), UserID: uuid.New()})
	from, to := teamWindow()

	got, err := svc.Team(ctx, nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	a := got[0]
	if a.Currency != "SAR" || a.CollectedAmt != 10_000+75_000 {
		t.Fatalf("collected = %d %s", a.CollectedAmt, a.Currency)
	}
	if len(a.Unconverted) != 1 || a.Unconverted[0] != "EUR" {
		t.Fatalf("unconverted = %v", a.Unconverted)
	}
	if a.Role != "employee" {
		t.Fatalf("role = %q", a.Role)
	}
	if o := got[1]; o.CollectedAmt != 0 || o.Currency != "SAR" || o.Unconverted == nil {
		t.Fatalf("empty member = %+v", o)
	}
}

func TestTeamWithoutReportingCurrencyKeepsDominantCurrency(t *testing.T) {
	svc := dashboard.NewService(&stubAgg{kpi: &dashboard.KPI{}, team: teamMembers()})
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelGlobal, UserID: uuid.New()})
	from, to := teamWindow()

	got, err := svc.Team(ctx, nil, from, to)
	if err != nil {
		t.Fatal(err)
	}
	a := got[0]
	if a.Currency != "USD" || a.CollectedAmt != 20_000 {
		t.Fatalf("collected = %d %s", a.CollectedAmt, a.Currency)
	}
	if len(a.Unconverted) != 2 {
		t.Fatalf("unconverted = %v", a.Unconverted)
	}
	if o := got[1]; o.Currency != "" || o.CollectedAmt != 0 {
		t.Fatalf("empty member = %+v", o)
	}
}
