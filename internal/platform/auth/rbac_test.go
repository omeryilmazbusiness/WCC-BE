package auth

import (
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func TestEffectiveHasCutsRoleByPlan(t *testing.T) {
	if !EffectiveHas(RoleGM, "standard", PermInboxWrite) || !EffectiveHas(RoleGM, "standard", PermAIWrite) {
		t.Fatal("standard gm keeps the starter desk")
	}
	if EffectiveHas(RoleGM, "standard", PermDashboardRead) || EffectiveHas(RoleGM, "standard", PermHotelsPricing) {
		t.Fatal("standard must hide dashboard and hotel cost")
	}
	if !EffectiveHas(RoleGM, "premium", PermPaymentsRead) || EffectiveHas(RoleGM, "premium", PermAISetup) {
		t.Fatal("premium adds money but not AI setup")
	}
	if !EffectiveHas(RoleGM, "ultimate", PermAISetup) || EffectiveHas(RoleGM, "ultimate", PermCompaniesManage) {
		t.Fatal("ultimate is full company, never platform")
	}
	if EffectiveHas(RoleGM, "", PermDashboardRead) {
		t.Fatal("unknown plan falls back to standard")
	}
	if !EffectiveHas(RoleAdmin, "standard", PermCompaniesManage) {
		t.Fatal("platform admin is not on a plan")
	}
}

func TestHasPermission(t *testing.T) {
	if !HasPermission(RoleGM, PermUsersWrite) {
		t.Fatal("gm should write users")
	}
	if HasPermission(RoleAgent, PermPaymentsRead) {
		t.Fatal("agent should not read payments by default")
	}
	if !HasPermission(RoleFinance, PermPaymentsWrite) {
		t.Fatal("finance should write payments")
	}
	if !ValidRole(RoleEmployee) || !ValidRole(RoleAgent) || ValidRole(Role("nope")) {
		t.Fatal("role validation")
	}
}

func TestCanonicalRoleMapsLegacyTitles(t *testing.T) {
	if CanonicalRole(RoleEmployee) != RoleAgent || CanonicalRole(RoleManager) != RoleAgent || CanonicalRole(RoleOperations) != RoleAgent {
		t.Fatal("legacy company titles become agent")
	}
	if CanonicalRole(RoleGM) != RoleGM || CanonicalRole(RoleFinance) != RoleFinance {
		t.Fatal("live titles stay")
	}
}

func TestAISetupNotLeaked(t *testing.T) {
	if HasPermission(RoleAgent, PermAISetup) {
		t.Fatal("agent must not setup AI")
	}
	if HasPermission(RoleFinance, PermAIWrite) {
		t.Fatal("finance must not write AI")
	}
	if !HasPermission(RoleGM, PermAISetup) {
		t.Fatal("gm must setup AI")
	}
}

func TestSettingsPermissions(t *testing.T) {
	if !HasPermission(RoleGM, PermSettingsWrite) {
		t.Fatal("gm must write settings")
	}
	if HasPermission(RoleAdmin, PermSettingsRead) {
		t.Fatal("company settings belong to the company, not the platform admin")
	}
	if !HasPermission(RoleFinance, PermSettingsRead) || HasPermission(RoleFinance, PermSettingsWrite) {
		t.Fatal("finance settings.read only")
	}
	if HasPermission(RoleAgent, PermSettingsRead) {
		t.Fatal("agent must not read settings")
	}
}

func TestSetupIsGMOnly(t *testing.T) {
	for _, r := range AllRoles() {
		if got := HasPermission(r, PermSetupManage); got != (r == RoleGM) {
			t.Fatalf("%s setup.manage = %v", r, got)
		}
	}
}

func TestCustomerAndPIIPermissions(t *testing.T) {
	for _, r := range CompanyRoles() {
		if !HasPermission(r, PermCustomersRead) {
			t.Fatalf("%s must read customers", r)
		}
	}
	if HasPermission(RoleAdmin, PermCustomersRead) || HasPermission(RoleFinance, PermCustomersWrite) {
		t.Fatal("admin has no customer access; finance is read-only")
	}
	if HasPermission(RoleAgent, PermPIIRead) || HasPermission(RoleFinance, PermPIIRead) {
		t.Fatal("agent and finance see masked passports only")
	}
	if !HasPermission(RoleGM, PermPIIRead) {
		t.Fatal("gm reads passports")
	}
}

func TestScopeLevels(t *testing.T) {
	team := uuid.New()
	cases := map[Role]access.Level{
		RoleGM: access.LevelCompany, RoleAdmin: access.LevelGlobal,
		RoleFinance: access.LevelBranch, RoleAgent: access.LevelOwn,
		RoleEmployee: access.LevelOwn,
	}
	for r, want := range cases {
		if got := ScopeLevelFor(r, team); got != want {
			t.Fatalf("%s: got %s want %s", r, got, want)
		}
	}
	if ScopeLevelFor(Role("x"), team) != access.LevelNone {
		t.Fatal("unknown role must be denied")
	}
}

func TestPrivacyManagePermission(t *testing.T) {
	for _, r := range AllRoles() {
		want := r == RoleGM
		if HasPermission(r, PermPrivacyManage) != want {
			t.Fatalf("%s privacy.manage = %v, want %v", r, !want, want)
		}
	}
}

func TestBookingOverrideAndDiscountPermissions(t *testing.T) {
	for _, r := range AllRoles() {
		want := r == RoleGM
		for _, p := range []Permission{PermBookingsOverride, PermBookingsDiscount} {
			if HasPermission(r, p) != want {
				t.Fatalf("%s %s = %v, want %v", r, p, !want, want)
			}
		}
	}
}

func TestFXManagePermission(t *testing.T) {
	for _, r := range AllRoles() {
		want := r == RoleGM || r == RoleFinance
		if HasPermission(r, PermFXManage) != want {
			t.Fatalf("%s fx.manage = %v, want %v", r, !want, want)
		}
	}
}

func TestPlatformAdminHoldsPlatformPermissionsOnly(t *testing.T) {
	want := map[Permission]bool{
		PermCompaniesManage: true, PermUsersRead: true, PermUsersWrite: true, PermUsersUnlock: true,
		PermAuditRead: true, PermOpsRead: true, PermSupportManage: true, PermNotificationsRead: true,
	}
	got := PermissionsFor(RoleAdmin)
	if len(got) != len(want) {
		t.Fatalf("admin has %d permissions, want %d: %v", len(got), len(want), got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("admin must not hold %s", p)
		}
	}
}

func TestHotelPricingIsContractingOnly(t *testing.T) {
	if !HasPermission(RoleGM, PermHotelsPricing) {
		t.Fatal("gm must set hotel prices")
	}
	for _, r := range []Role{RoleAgent, RoleFinance, RoleAdmin} {
		if HasPermission(r, PermHotelsPricing) {
			t.Fatalf("%s must not set hotel prices", r)
		}
	}
}

func TestBusesPermissions(t *testing.T) {
	if !HasPermission(RoleGM, PermBusesRead) || !HasPermission(RoleGM, PermBusesWrite) {
		t.Fatal("gm must read and write buses")
	}
	if !HasPermission(RoleAgent, PermBusesRead) || HasPermission(RoleAgent, PermBusesWrite) {
		t.Fatal("agent buses.read only")
	}
	if !HasPermission(RoleFinance, PermBusesRead) || HasPermission(RoleFinance, PermBusesWrite) {
		t.Fatal("finance buses.read only")
	}
	if HasPermission(RoleAdmin, PermBusesRead) {
		t.Fatal("platform admin has no company bus catalogue")
	}
}

func TestSupportPermissions(t *testing.T) {
	for _, r := range AllRoles() {
		if HasPermission(r, PermSupportWrite) != (r != RoleAdmin) {
			t.Fatalf("%s support.write", r)
		}
		if HasPermission(r, PermSupportManage) != (r == RoleAdmin) {
			t.Fatalf("%s support.manage", r)
		}
	}
}
