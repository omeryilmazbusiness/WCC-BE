package auth

import "testing"

func TestForcedMFARolesArePlatformAdminOnly(t *testing.T) {
	p := NewMFAPolicy(ForcedMFARoles()...)
	if !p.Required(RoleAdmin) {
		t.Fatal("platform admin must enroll MFA at first sign-in")
	}
	for _, role := range []Role{RoleGM, RoleFinance, RoleAgent, RoleManager, RoleEmployee, RoleOperations} {
		if p.Required(role) {
			t.Errorf("company role %s must sign in with password only", role)
		}
	}
}
