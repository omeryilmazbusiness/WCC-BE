package auth

// Permission is a fine-grained capability enforced at the API boundary.
type Permission string

const (
	PermUsersRead       Permission = "users.read"
	PermUsersWrite      Permission = "users.write"
	PermRolesRead       Permission = "roles.read"
	PermAuditRead       Permission = "audit.read"
	PermBranchesRead    Permission = "branches.read"
	PermCustomersWrite  Permission = "customers.write"
	PermLeadsRead       Permission = "leads.read"
	PermLeadsWrite      Permission = "leads.write"
	PermBookingsWrite   Permission = "bookings.write"
	PermPaymentsWrite   Permission = "payments.write"
	PermPaymentsRead    Permission = "payments.read"
	PermDocsWrite       Permission = "documents.write"
	PermOpsRead         Permission = "ops.read"
	PermDashboardRead   Permission = "dashboard.read"
	PermPackagesWrite   Permission = "packages.write"
	PermTasksWrite      Permission = "tasks.write"
)

const (
	RoleGM         Role = "gm"
	RoleManager    Role = "manager"
	RoleEmployee   Role = "employee"
	RoleFinance    Role = "finance"
	RoleOperations Role = "operations"
	RoleAdmin      Role = "admin"
)

// AllRoles is the canonical role catalog (PDF §3).
func AllRoles() []Role {
	return []Role{RoleGM, RoleManager, RoleEmployee, RoleFinance, RoleOperations, RoleAdmin}
}

func ValidRole(r Role) bool {
	for _, x := range AllRoles() {
		if x == r {
			return true
		}
	}
	return false
}

// PermissionsFor returns the static RBAC matrix for a role.
func PermissionsFor(role Role) []Permission {
	switch role {
	case RoleGM:
		return []Permission{
			PermUsersRead, PermUsersWrite, PermRolesRead, PermAuditRead, PermBranchesRead,
			PermCustomersWrite, PermLeadsRead, PermLeadsWrite, PermBookingsWrite,
			PermPaymentsRead, PermPaymentsWrite, PermDocsWrite, PermOpsRead,
			PermDashboardRead, PermPackagesWrite, PermTasksWrite,
		}
	case RoleAdmin:
		return []Permission{
			PermUsersRead, PermUsersWrite, PermRolesRead, PermAuditRead, PermBranchesRead, PermOpsRead,
		}
	case RoleManager:
		return []Permission{
			PermUsersRead, PermRolesRead, PermBranchesRead, PermAuditRead,
			PermCustomersWrite, PermLeadsRead, PermLeadsWrite, PermBookingsWrite,
			PermPaymentsRead, PermPaymentsWrite, PermDocsWrite,
			PermDashboardRead, PermPackagesWrite, PermTasksWrite,
		}
	case RoleEmployee:
		return []Permission{
			PermBranchesRead, PermCustomersWrite, PermLeadsRead, PermLeadsWrite, PermBookingsWrite,
			PermDocsWrite, PermTasksWrite,
		}
	case RoleFinance:
		return []Permission{
			PermBranchesRead, PermPaymentsRead, PermPaymentsWrite, PermBookingsWrite, PermAuditRead,
		}
	case RoleOperations:
		return []Permission{
			PermBranchesRead, PermDocsWrite, PermBookingsWrite, PermPackagesWrite, PermTasksWrite,
		}
	default:
		return nil
	}
}

func HasPermission(role Role, p Permission) bool {
	for _, x := range PermissionsFor(role) {
		if x == p {
			return true
		}
	}
	return false
}

// RolePermissionMatrix is the payload for the admin matrix UI.
func RolePermissionMatrix() map[Role][]Permission {
	out := make(map[Role][]Permission, len(AllRoles()))
	for _, r := range AllRoles() {
		out[r] = PermissionsFor(r)
	}
	return out
}
