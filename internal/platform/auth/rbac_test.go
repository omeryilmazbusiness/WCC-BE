package auth

import (
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func TestHasPermission(t *testing.T) {
	if !HasPermission(RoleGM, PermUsersWrite) {
		t.Fatal("gm should write users")
	}
	if HasPermission(RoleEmployee, PermPaymentsRead) {
		t.Fatal("employee should not read payments by default")
	}
	if !HasPermission(RoleFinance, PermPaymentsWrite) {
		t.Fatal("finance should write payments")
	}
	if !ValidRole(RoleOperations) || ValidRole(Role("nope")) {
		t.Fatal("role validation")
	}
}

// Epic 17 T-210 — AI setup must not leak to non-privileged roles.
func TestAISetupNotLeaked(t *testing.T) {
	if HasPermission(RoleEmployee, PermAISetup) {
		t.Fatal("employee must not setup AI")
	}
	if HasPermission(RoleFinance, PermAIWrite) {
		t.Fatal("finance must not write AI")
	}
	if !HasPermission(RoleGM, PermAISetup) {
		t.Fatal("gm must setup AI")
	}
}

// Epic 18 — settings permissions matrix.
func TestSettingsPermissions(t *testing.T) {
	if !HasPermission(RoleGM, PermSettingsWrite) || !HasPermission(RoleAdmin, PermSettingsWrite) || !HasPermission(RoleManager, PermSettingsWrite) {
		t.Fatal("gm/admin/manager must write settings")
	}
	if !HasPermission(RoleOperations, PermSettingsRead) || HasPermission(RoleOperations, PermSettingsWrite) {
		t.Fatal("operations settings.read only")
	}
	if !HasPermission(RoleFinance, PermSettingsRead) || HasPermission(RoleFinance, PermSettingsWrite) {
		t.Fatal("finance settings.read only")
	}
	if HasPermission(RoleEmployee, PermSettingsRead) {
		t.Fatal("employee must not read settings")
	}
}

func TestCustomerAndPIIPermissions(t *testing.T) {
	for _, r := range []Role{RoleGM, RoleManager, RoleEmployee, RoleFinance, RoleOperations} {
		if !HasPermission(r, PermCustomersRead) {
			t.Fatalf("%s must read customers", r)
		}
	}
	if HasPermission(RoleAdmin, PermCustomersRead) || HasPermission(RoleFinance, PermCustomersWrite) {
		t.Fatal("admin has no customer access; finance is read-only")
	}
	if HasPermission(RoleEmployee, PermPIIRead) || HasPermission(RoleFinance, PermPIIRead) {
		t.Fatal("employee and finance see masked passports only")
	}
	if !HasPermission(RoleOperations, PermPIIRead) {
		t.Fatal("operations needs passports for visa work")
	}
}

func TestScopeLevels(t *testing.T) {
	team := uuid.New()
	cases := map[Role]access.Level{
		RoleGM: access.LevelGlobal, RoleAdmin: access.LevelGlobal,
		RoleFinance: access.LevelBranch, RoleOperations: access.LevelBranch,
		RoleManager: access.LevelTeam, RoleEmployee: access.LevelOwn,
	}
	for r, want := range cases {
		if got := ScopeLevelFor(r, team); got != want {
			t.Fatalf("%s: got %s want %s", r, got, want)
		}
	}
	if ScopeLevelFor(RoleManager, uuid.Nil) != access.LevelBranch {
		t.Fatal("manager without team falls back to branch")
	}
	if ScopeLevelFor(Role("x"), team) != access.LevelNone {
		t.Fatal("unknown role must be denied")
	}
}
