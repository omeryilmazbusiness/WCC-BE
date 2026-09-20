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
