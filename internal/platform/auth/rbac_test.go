package auth

import "testing"

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
