package hardening

import (
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// AILeakageMatrix documents which roles must NOT receive elevated AI capabilities (T-210).
// Setup is GM/Admin/Manager only; Finance never writes AI; Employee never setups.
func TestAIPermissionLeakage(t *testing.T) {
	cases := []struct {
		role auth.Role
		perm auth.Permission
		want bool
	}{
		{auth.RoleEmployee, auth.PermAISetup, false},
		{auth.RoleFinance, auth.PermAISetup, false},
		{auth.RoleFinance, auth.PermAIWrite, false},
		{auth.RoleOperations, auth.PermAISetup, false},
		{auth.RoleGM, auth.PermAISetup, true},
		{auth.RoleManager, auth.PermAISetup, true},
		{auth.RoleAdmin, auth.PermAISetup, true},
		{auth.RoleEmployee, auth.PermAIRead, true},
		{auth.RoleEmployee, auth.PermAIWrite, true},
		{auth.RoleFinance, auth.PermAIRead, true},
	}
	for _, tc := range cases {
		got := auth.HasPermission(tc.role, tc.perm)
		if got != tc.want {
			t.Fatalf("%s %s: got %v want %v", tc.role, tc.perm, got, tc.want)
		}
	}
}

func TestRolePermissionMatrixCoverage(t *testing.T) {
	matrix := auth.RolePermissionMatrix()
	roles := auth.AllRoles()
	if len(matrix) != len(roles) {
		t.Fatalf("matrix size %d want %d", len(matrix), len(roles))
	}
	// Critical commercial perms must exist for GM.
	required := []auth.Permission{
		auth.PermLeadsWrite, auth.PermBookingsWrite, auth.PermPaymentsApprove,
		auth.PermDocsReview, auth.PermImportsWrite, auth.PermReportsExport,
		auth.PermFileSyncWrite, auth.PermIntegrationsWrite, auth.PermAISetup,
	}
	for _, p := range required {
		if !auth.HasPermission(auth.RoleGM, p) {
			t.Fatalf("gm missing %s", p)
		}
	}
	// Employee must not approve payments or manage users.
	denied := []auth.Permission{
		auth.PermPaymentsApprove, auth.PermUsersWrite, auth.PermAuditRead,
		auth.PermNotificationsManage,
	}
	for _, p := range denied {
		if auth.HasPermission(auth.RoleEmployee, p) {
			t.Fatalf("employee must not have %s", p)
		}
	}
}

func TestWorkflowJourney(t *testing.T) {
	j := CanonicalJourney()
	if len(j) != 7 {
		t.Fatalf("journey len=%d", len(j))
	}
	if !CanAdvance(StageInboundMessage, StageLead) {
		t.Fatal("inbound→lead")
	}
	if CanAdvance(StageInboundMessage, StageBooking) {
		t.Fatal("must not skip lead")
	}
	if !CanAdvance(StageLead, StageLead) {
		t.Fatal("same stage ok")
	}
	seen := map[WorkflowStage]bool{}
	for _, s := range j {
		seen[s] = true
	}
	if !JourneyComplete(seen) {
		t.Fatal("complete")
	}
	delete(seen, StageReport)
	if JourneyComplete(seen) {
		t.Fatal("incomplete without report")
	}
}

func TestIdempotencyKey(t *testing.T) {
	if NormalizeIdempotencyKey("short") != "" {
		t.Fatal("too short")
	}
	ok := NormalizeIdempotencyKey("payment:booking-1:charge-1")
	if ok == "" {
		t.Fatal("valid key rejected")
	}
	if NormalizeIdempotencyKey("  payment:booking-1:charge-1  ") != ok {
		t.Fatal("trim")
	}
	if !SameEffect("abc", "abc") || SameEffect("a", "b") || SameEffect("", "") {
		t.Fatal("SameEffect")
	}
}
