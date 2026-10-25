package company

import "testing"

func TestParsePlan(t *testing.T) {
	if p, ok := ParsePlan(" Premium "); !ok || p != PlanPremium {
		t.Fatalf("got %q %v", p, ok)
	}
	if _, ok := ParsePlan("gold"); ok {
		t.Fatal("unknown plan")
	}
}

func TestStandardIsACappedStarterDesk(t *testing.T) {
	if !Allows(PlanStandard, "inbox.write") || !Allows(PlanStandard, "packages.write") || !Allows(PlanStandard, "ai.write") {
		t.Fatal("standard must include the starter modules")
	}
	for _, blocked := range []string{
		"dashboard.read", "payments.read", "reports.read", "suppliers.read", "visa.read",
		"ai.setup", "hotels.pricing", "bookings.discount", "bookings.override",
		"targets.write", "fx.manage", "privacy.manage", "companies.manage", "leads.delete",
	} {
		if Allows(PlanStandard, blocked) {
			t.Fatalf("standard must not include %s", blocked)
		}
	}
	if err := Refuse(PlanStandard, LimitUsers, 2); err != nil {
		t.Fatal(err)
	}
	if err := Refuse(PlanStandard, LimitUsers, 3); err == nil {
		t.Fatal("fourth seat must be refused")
	}
	if err := Refuse(PlanStandard, LimitBranches, 1); err == nil {
		t.Fatal("second branch must be refused")
	}
}

func TestPremiumAddsMoneyAndOps(t *testing.T) {
	if !Allows(PlanPremium, "payments.approve") || !Allows(PlanPremium, "hotels.pricing") || !Allows(PlanPremium, "reports.read") {
		t.Fatal("premium money/ops")
	}
	if Allows(PlanPremium, "ai.setup") || Allows(PlanPremium, "bookings.override") || Allows(PlanPremium, "fx.manage") {
		t.Fatal("premium is not ultimate")
	}
	if err := Refuse(PlanPremium, LimitUsers, 11); err != nil {
		t.Fatal(err)
	}
	if err := Refuse(PlanPremium, LimitUsers, 12); err == nil {
		t.Fatal("13th seat")
	}
}

func TestUltimateIsUnlimitedCompanyAccess(t *testing.T) {
	if !Allows(PlanUltimate, "ai.setup") || !Allows(PlanUltimate, "fx.manage") || !Allows(PlanUltimate, "privacy.manage") {
		t.Fatal("ultimate company perms")
	}
	if Allows(PlanUltimate, "companies.manage") || Allows(PlanUltimate, "support.manage") {
		t.Fatal("platform perms stay off the company plan")
	}
	if err := Refuse(PlanUltimate, LimitUsers, 500); err != nil {
		t.Fatal(err)
	}
	if Of("").Plan != PlanStandard {
		t.Fatal("unknown plan must not escalate")
	}
}

func TestUpgradeLadder(t *testing.T) {
	if next, ok := NextPlan(PlanStandard); !ok || next != PlanPremium {
		t.Fatal("standard steps to premium")
	}
	if next, ok := NextPlan(PlanPremium); !ok || next != PlanUltimate {
		t.Fatal("premium steps to ultimate")
	}
	if _, ok := NextPlan(PlanUltimate); ok {
		t.Fatal("ultimate has no next")
	}
	if !CanUpgradeTo(PlanStandard, PlanUltimate) || CanUpgradeTo(PlanPremium, PlanStandard) {
		t.Fatal("upgrade is strictly up")
	}
	if p, ok := CheapestPlanFor("dashboard.read"); !ok || p != PlanPremium {
		t.Fatal("dashboard starts at premium")
	}
	if p, ok := CheapestPlanFor("ai.setup"); !ok || p != PlanUltimate {
		t.Fatal("ai setup is ultimate")
	}
	if _, ok := CheapestPlanFor("companies.manage"); ok {
		t.Fatal("platform perms have no plan")
	}
	if UpgradePage(PlanPremium) != "/upgrade/premium" {
		t.Fatal("dedupe page")
	}
}
