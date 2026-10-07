package auth

// Permission is a fine-grained capability enforced at the API boundary.
type Permission string

const (
	PermUsersRead           Permission = "users.read"
	PermUsersWrite          Permission = "users.write"
	PermRolesRead           Permission = "roles.read"
	PermAuditRead           Permission = "audit.read"
	PermBranchesRead        Permission = "branches.read"
	PermCustomersRead       Permission = "customers.read"
	PermCustomersWrite      Permission = "customers.write"
	PermPIIRead             Permission = "pii.read"
	PermUsersUnlock         Permission = "users.unlock"
	PermLeadsRead           Permission = "leads.read"
	PermLeadsWrite          Permission = "leads.write"
	PermLeadsDelete         Permission = "leads.delete"
	PermBookingsRead        Permission = "bookings.read"
	PermBookingsWrite       Permission = "bookings.write"
	PermBookingsOverride    Permission = "bookings.override"
	PermBookingsDiscount    Permission = "bookings.discount"
	PermPaymentsWrite       Permission = "payments.write"
	PermPaymentsRead        Permission = "payments.read"
	PermPaymentsApprove     Permission = "payments.approve"
	PermFXManage            Permission = "fx.manage"
	PermDocsRead            Permission = "documents.read"
	PermDocsWrite           Permission = "documents.write"
	PermDocsReview          Permission = "documents.review"
	PermVisaRead            Permission = "visa.read"
	PermVisaWrite           Permission = "visa.write"
	PermSuppliersRead       Permission = "suppliers.read"
	PermSuppliersWrite      Permission = "suppliers.write"
	PermSuppliersFinance    Permission = "suppliers.finance"
	PermHotelsRead          Permission = "hotels.read"
	PermHotelsWrite         Permission = "hotels.write"
	PermOpsRead             Permission = "ops.read"
	PermDashboardRead       Permission = "dashboard.read"
	PermPackagesRead        Permission = "packages.read"
	PermPackagesWrite       Permission = "packages.write"
	PermTasksRead           Permission = "tasks.read"
	PermTasksWrite          Permission = "tasks.write"
	PermInboxRead           Permission = "inbox.read"
	PermInboxWrite          Permission = "inbox.write"
	PermIntegrationsRead    Permission = "integrations.read"
	PermIntegrationsWrite   Permission = "integrations.write"
	PermTargetsRead         Permission = "targets.read"
	PermTargetsWrite        Permission = "targets.write"
	PermImportsRead         Permission = "imports.read"
	PermImportsWrite        Permission = "imports.write"
	PermNotificationsRead   Permission = "notifications.read"
	PermNotificationsWrite  Permission = "notifications.write"
	PermNotificationsManage Permission = "notifications.manage"
	PermReportsRead         Permission = "reports.read"
	PermReportsExport       Permission = "reports.export"
	PermAIRead              Permission = "ai.read"
	PermAIWrite             Permission = "ai.write"
	PermAISetup             Permission = "ai.setup"
	PermSettingsRead        Permission = "settings.read"
	PermSettingsWrite       Permission = "settings.write"
	// PermPrivacyManage serves KVKK data subject requests (export, anonymize).
	PermPrivacyManage Permission = "privacy.manage"
	// PermSetupManage runs the first-run company onboarding.
	PermSetupManage Permission = "setup.manage"
	// PermBranchesManage creates, renames and re-kinds the company's branches.
	PermBranchesManage Permission = "branches.manage"
	// PermCompaniesManage registers tenant companies (platform operators only).
	PermCompaniesManage Permission = "companies.manage"
	// PermFlightsSearch looks up flight fares for customers.
	PermFlightsSearch Permission = "flights.search"
	// Help requests: every company user can send one; the platform team works them.
	PermSupportWrite  Permission = "support.write"
	PermSupportManage Permission = "support.manage"
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
			PermCustomersRead, PermCustomersWrite, PermPIIRead, PermUsersUnlock, PermLeadsRead, PermLeadsWrite, PermLeadsDelete, PermBookingsRead, PermBookingsWrite,
			PermPaymentsRead, PermPaymentsWrite, PermPaymentsApprove,
			PermDocsRead, PermDocsWrite, PermDocsReview, PermVisaRead, PermVisaWrite,
			PermSuppliersRead, PermSuppliersWrite, PermSuppliersFinance, PermOpsRead, PermHotelsRead, PermHotelsWrite,
			PermDashboardRead, PermPackagesRead, PermPackagesWrite, PermTasksRead, PermTasksWrite,
			PermInboxRead, PermInboxWrite, PermIntegrationsRead, PermIntegrationsWrite,
			PermTargetsRead, PermTargetsWrite, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite, PermNotificationsManage,
			PermReportsRead, PermReportsExport,
			PermAIRead, PermAIWrite, PermAISetup,
			PermSettingsRead, PermSettingsWrite,
			PermPrivacyManage,
			PermFXManage,
			PermBookingsOverride, PermBookingsDiscount,
			PermSetupManage, PermBranchesManage,
			PermFlightsSearch,
			PermSupportWrite,
		}
	case RoleAdmin:
		return []Permission{
			// Platform operator: tenants, accounts and platform health. Company
			// data and configuration belong to each company's GM.
			PermCompaniesManage,
			PermUsersRead, PermUsersWrite, PermUsersUnlock, PermAuditRead, PermOpsRead,
			PermSupportManage,
		}
	case RoleManager:
		return []Permission{
			PermUsersRead, PermRolesRead, PermBranchesRead, PermAuditRead,
			PermCustomersRead, PermCustomersWrite, PermPIIRead, PermLeadsRead, PermLeadsWrite, PermLeadsDelete, PermBookingsRead, PermBookingsWrite,
			PermPaymentsRead, PermPaymentsWrite, PermPaymentsApprove,
			PermDocsRead, PermDocsWrite, PermDocsReview, PermVisaRead, PermVisaWrite,
			PermSuppliersRead, PermSuppliersWrite, PermSuppliersFinance, PermHotelsRead, PermHotelsWrite,
			PermDashboardRead, PermPackagesRead, PermPackagesWrite, PermTasksRead, PermTasksWrite,
			PermInboxRead, PermInboxWrite, PermIntegrationsRead, PermIntegrationsWrite,
			PermTargetsRead, PermTargetsWrite, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite, PermNotificationsManage,
			PermReportsRead, PermReportsExport,
			PermAIRead, PermAIWrite, PermAISetup,
			PermSettingsRead, PermSettingsWrite,
			PermBookingsOverride, PermBookingsDiscount,
			PermFlightsSearch,
			PermSupportWrite,
		}
	case RoleEmployee:
		return []Permission{
			PermBranchesRead, PermCustomersRead, PermCustomersWrite, PermLeadsRead, PermLeadsWrite, PermBookingsRead, PermBookingsWrite,
			PermDocsRead, PermDocsWrite, PermVisaRead, PermVisaWrite, PermSuppliersRead, PermHotelsRead,
			PermTasksRead, PermTasksWrite, PermPackagesRead,
			PermInboxRead, PermInboxWrite, PermTargetsRead, PermImportsRead,
			PermNotificationsRead, PermNotificationsWrite,
			PermReportsRead,
			PermAIRead, PermAIWrite,
			PermFlightsSearch,
			PermSupportWrite,
		}
	case RoleFinance:
		return []Permission{
			PermBranchesRead, PermCustomersRead, PermPaymentsRead, PermPaymentsWrite, PermPaymentsApprove, PermBookingsRead, PermBookingsWrite, PermAuditRead,
			PermDocsRead, PermSuppliersRead, PermSuppliersFinance, PermHotelsRead,
			PermTasksRead, PermTargetsRead, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite,
			PermReportsRead, PermReportsExport,
			PermAIRead,
			PermSettingsRead,
			PermFXManage,
			PermSupportWrite,
		}
	case RoleOperations:
		return []Permission{
			PermBranchesRead, PermCustomersRead, PermCustomersWrite, PermPIIRead,
			PermDocsRead, PermDocsWrite, PermDocsReview, PermVisaRead, PermVisaWrite,
			PermSuppliersRead, PermSuppliersWrite, PermHotelsRead, PermHotelsWrite,
			PermBookingsRead, PermBookingsWrite, PermPackagesRead, PermPackagesWrite, PermTasksRead, PermTasksWrite,
			PermInboxRead, PermInboxWrite, PermIntegrationsRead, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite,
			PermReportsRead, PermReportsExport,
			PermAIRead, PermAIWrite,
			PermSettingsRead,
			PermFlightsSearch,
			PermSupportWrite,
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
