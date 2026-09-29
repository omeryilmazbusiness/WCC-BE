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
	PermFileSyncRead        Permission = "filesync.read"
	PermFileSyncWrite       Permission = "filesync.write"
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
			PermCustomersRead, PermCustomersWrite, PermPIIRead, PermUsersUnlock, PermLeadsRead, PermLeadsWrite, PermBookingsRead, PermBookingsWrite,
			PermPaymentsRead, PermPaymentsWrite, PermPaymentsApprove,
			PermDocsRead, PermDocsWrite, PermDocsReview, PermVisaRead, PermVisaWrite,
			PermSuppliersRead, PermSuppliersWrite, PermOpsRead,
			PermDashboardRead, PermPackagesRead, PermPackagesWrite, PermTasksRead, PermTasksWrite,
			PermInboxRead, PermInboxWrite, PermIntegrationsRead, PermIntegrationsWrite,
			PermTargetsRead, PermTargetsWrite, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite, PermNotificationsManage,
			PermReportsRead, PermReportsExport,
			PermAIRead, PermAIWrite, PermAISetup,
			PermFileSyncRead, PermFileSyncWrite,
			PermSettingsRead, PermSettingsWrite,
			PermPrivacyManage,
			PermFXManage,
			PermBookingsOverride, PermBookingsDiscount,
			PermSetupManage, PermBranchesManage,
		}
	case RoleAdmin:
		return []Permission{
			PermUsersRead, PermUsersWrite, PermUsersUnlock, PermRolesRead, PermAuditRead, PermBranchesRead, PermOpsRead,
			PermIntegrationsRead, PermIntegrationsWrite,
			PermNotificationsRead, PermNotificationsWrite, PermNotificationsManage,
			PermReportsRead, PermReportsExport,
			PermAIRead, PermAIWrite, PermAISetup,
			PermFileSyncRead, PermFileSyncWrite,
			PermSettingsRead, PermSettingsWrite,
			PermPrivacyManage,
			PermBranchesManage, PermCompaniesManage,
		}
	case RoleManager:
		return []Permission{
			PermUsersRead, PermRolesRead, PermBranchesRead, PermAuditRead,
			PermCustomersRead, PermCustomersWrite, PermPIIRead, PermLeadsRead, PermLeadsWrite, PermBookingsRead, PermBookingsWrite,
			PermPaymentsRead, PermPaymentsWrite, PermPaymentsApprove,
			PermDocsRead, PermDocsWrite, PermDocsReview, PermVisaRead, PermVisaWrite,
			PermSuppliersRead, PermSuppliersWrite,
			PermDashboardRead, PermPackagesRead, PermPackagesWrite, PermTasksRead, PermTasksWrite,
			PermInboxRead, PermInboxWrite, PermIntegrationsRead, PermIntegrationsWrite,
			PermTargetsRead, PermTargetsWrite, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite, PermNotificationsManage,
			PermReportsRead, PermReportsExport,
			PermAIRead, PermAIWrite, PermAISetup,
			PermFileSyncRead, PermFileSyncWrite,
			PermSettingsRead, PermSettingsWrite,
			PermBookingsOverride, PermBookingsDiscount,
		}
	case RoleEmployee:
		return []Permission{
			PermBranchesRead, PermCustomersRead, PermCustomersWrite, PermLeadsRead, PermLeadsWrite, PermBookingsRead, PermBookingsWrite,
			PermDocsRead, PermDocsWrite, PermVisaRead, PermVisaWrite, PermSuppliersRead,
			PermTasksRead, PermTasksWrite, PermPackagesRead,
			PermInboxRead, PermInboxWrite, PermTargetsRead, PermImportsRead,
			PermNotificationsRead, PermNotificationsWrite,
			PermReportsRead,
			PermAIRead, PermAIWrite,
			PermFileSyncRead,
		}
	case RoleFinance:
		return []Permission{
			PermBranchesRead, PermCustomersRead, PermPaymentsRead, PermPaymentsWrite, PermPaymentsApprove, PermBookingsRead, PermBookingsWrite, PermAuditRead,
			PermDocsRead, PermSuppliersRead,
			PermTasksRead, PermTargetsRead, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite,
			PermReportsRead, PermReportsExport,
			PermAIRead,
			PermFileSyncRead,
			PermSettingsRead,
			PermFXManage,
		}
	case RoleOperations:
		return []Permission{
			PermBranchesRead, PermCustomersRead, PermCustomersWrite, PermPIIRead,
			PermDocsRead, PermDocsWrite, PermDocsReview, PermVisaRead, PermVisaWrite,
			PermSuppliersRead, PermSuppliersWrite,
			PermBookingsRead, PermBookingsWrite, PermPackagesRead, PermPackagesWrite, PermTasksRead, PermTasksWrite,
			PermInboxRead, PermInboxWrite, PermIntegrationsRead, PermImportsRead, PermImportsWrite,
			PermNotificationsRead, PermNotificationsWrite,
			PermReportsRead, PermReportsExport,
			PermAIRead, PermAIWrite,
			PermFileSyncRead, PermFileSyncWrite,
			PermSettingsRead,
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
