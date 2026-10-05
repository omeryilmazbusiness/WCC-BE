package notification

import "time"

// Rule is a pure escalation matrix row (T-174).
// EscalateAfter = 0 means notify immediately; >0 means bump/escalate after age.
type Rule struct {
	Kind            string
	Severity        Severity
	EscalateAfter   time.Duration
	EscalateToRoles []string // manager / gm / operations / finance
	Groupable       bool
	DefaultTitle    string
	DefaultHref     string
	EntityType      string
}

// DefaultRules is the canonical escalation matrix.
func DefaultRules() []Rule {
	return []Rule{
		{
			Kind: KindMessageSLA, Severity: SeverityCritical,
			EscalateAfter: 15 * time.Minute, EscalateToRoles: []string{"manager", "gm"},
			Groupable: true, DefaultTitle: "Conversation SLA breached",
			DefaultHref: "/inbox", EntityType: "conversation",
		},
		{
			Kind: KindLeadNoFollowUp, Severity: SeverityWarning,
			EscalateAfter: 24 * time.Hour, EscalateToRoles: []string{"manager"},
			Groupable: true, DefaultTitle: "Lead without follow-up",
			DefaultHref: "/pipeline", EntityType: "lead",
		},
		{
			Kind: KindTaskOverdue, Severity: SeverityWarning,
			EscalateAfter: 2 * time.Hour, EscalateToRoles: []string{"manager"},
			Groupable: true, DefaultTitle: "Task overdue",
			DefaultHref: "/tasks", EntityType: "task",
		},
		{
			Kind: KindTaskEscalated, Severity: SeverityCritical,
			EscalateAfter: 0, EscalateToRoles: []string{"manager", "gm"},
			Groupable: true, DefaultTitle: "Task escalated",
			DefaultHref: "/tasks", EntityType: "task",
		},
		{
			Kind: KindPaymentOverdue, Severity: SeverityCritical,
			EscalateAfter: 12 * time.Hour, EscalateToRoles: []string{"finance", "manager"},
			Groupable: true, DefaultTitle: "Payment overdue",
			DefaultHref: "/finance", EntityType: "booking",
		},
		{
			Kind: KindDocumentMissing, Severity: SeverityWarning,
			EscalateAfter: 24 * time.Hour, EscalateToRoles: []string{"operations", "manager"},
			Groupable: true, DefaultTitle: "Missing document",
			DefaultHref: "/missing-docs", EntityType: "booking",
		},
		{
			Kind: KindDocumentExpiring, Severity: SeverityWarning,
			EscalateAfter: 48 * time.Hour, EscalateToRoles: []string{"operations"},
			Groupable: true, DefaultTitle: "Document expiring",
			DefaultHref: "/missing-docs", EntityType: "document",
		},
		{
			Kind: KindTargetBehind, Severity: SeverityWarning,
			EscalateAfter: 24 * time.Hour, EscalateToRoles: []string{"manager", "gm"},
			Groupable: true, DefaultTitle: "Revenue target behind pace",
			DefaultHref: "/targets", EntityType: "target",
		},
		{
			Kind: KindIntegrationDown, Severity: SeverityCritical,
			EscalateAfter: 30 * time.Minute, EscalateToRoles: []string{"gm", "manager"},
			Groupable: true, DefaultTitle: "Integration unhealthy",
			DefaultHref: "/inbox", EntityType: "integration",
		},
		{
			Kind: KindBookingConfirmed, Severity: SeverityInfo,
			EscalateAfter: 0, EscalateToRoles: nil,
			Groupable: false, DefaultTitle: "Booking confirmed",
			DefaultHref: "/bookings", EntityType: "booking",
		},
		{
			Kind: KindSupplierUnconfirmed, Severity: SeverityWarning,
			EscalateAfter: 24 * time.Hour, EscalateToRoles: []string{"operations", "manager"},
			Groupable: true, DefaultTitle: "Supplier unconfirmed",
			DefaultHref: "/suppliers", EntityType: "supplier",
		},
		{
			Kind: KindSupplierLowBalance, Severity: SeverityWarning,
			EscalateAfter: 12 * time.Hour, EscalateToRoles: []string{"manager", "gm"},
			Groupable: true, DefaultTitle: "Supplier balance low",
			DefaultHref: "/suppliers", EntityType: "supplier",
		},
		{
			Kind: KindSupplierContract, Severity: SeverityWarning,
			EscalateAfter: 72 * time.Hour, EscalateToRoles: []string{"manager"},
			Groupable: true, DefaultTitle: "Supplier contract ending",
			DefaultHref: "/suppliers", EntityType: "supplier",
		},
		{
			Kind: KindMessageSLAWarning, Severity: SeverityWarning,
			EscalateAfter: 0, EscalateToRoles: nil,
			Groupable: true, DefaultTitle: "Conversation nearing SLA",
			DefaultHref: "/inbox", EntityType: "conversation",
		},
		{
			Kind: KindPaymentDue, Severity: SeverityWarning,
			EscalateAfter: 24 * time.Hour, EscalateToRoles: []string{"finance"},
			Groupable: true, DefaultTitle: "Payment due soon",
			DefaultHref: "/finance", EntityType: "booking",
		},
		{
			Kind: KindPassportExpiring, Severity: SeverityWarning,
			EscalateAfter: 72 * time.Hour, EscalateToRoles: []string{"operations"},
			Groupable: true, DefaultTitle: "Passport expiring",
			DefaultHref: "/customers", EntityType: "customer",
		},
		{
			Kind: KindVisaFollowUp, Severity: SeverityWarning,
			EscalateAfter: 48 * time.Hour, EscalateToRoles: []string{"operations", "manager"},
			Groupable: true, DefaultTitle: "Visa awaiting decision",
			DefaultHref: "/bookings", EntityType: "visa_application",
		},
		{
			Kind: KindReportReady, Severity: SeverityInfo,
			Groupable: false, DefaultTitle: "Scheduled report ready",
			DefaultHref: "/reports", EntityType: "report_run",
		},
		{
			Kind: KindImportCompleted, Severity: SeverityInfo,
			Groupable: false, DefaultTitle: "Import finished",
			DefaultHref: "/import-export", EntityType: "import_job",
		},
		{
			Kind: KindAISummary, Severity: SeverityInfo,
			Groupable: true, DefaultTitle: "Daily AI summary",
			DefaultHref: "/manager", EntityType: "branch",
		},
		{
			Kind: KindAILostLeads, Severity: SeverityInfo,
			Groupable: true, DefaultTitle: "Weekly lost leads analysis",
			DefaultHref: "/manager", EntityType: "branch",
		},
		{
			Kind: KindTaskReminder, Severity: SeverityInfo,
			Groupable: false, DefaultTitle: "Task reminder",
			DefaultHref: "/tasks", EntityType: "task",
		},
	}
}

// Escalates reports whether the rule promotes alerts at all.
func (r Rule) Escalates() bool {
	return r.EscalateAfter > 0 && len(r.EscalateToRoles) > 0
}

// Due reports whether an alert created at createdAt is past the rule's window.
func (r Rule) Due(createdAt, now time.Time) bool {
	return r.Escalates() && !createdAt.Add(r.EscalateAfter).After(now)
}

// RulesByKind indexes a rule set.
func RulesByKind(rules []Rule) map[string]Rule {
	out := make(map[string]Rule, len(rules))
	for _, r := range rules {
		out[r.Kind] = r
	}
	return out
}

// MatchRule returns the matrix row for a kind, or nil if unknown.
func MatchRule(kind string) *Rule {
	for i := range defaultRulesCache {
		if defaultRulesCache[i].Kind == kind {
			r := defaultRulesCache[i]
			return &r
		}
	}
	return nil
}

var defaultRulesCache = DefaultRules()

// ShouldEscalate reports whether an open alert of this kind is past the grace window.
func ShouldEscalate(kind string, createdAt, now time.Time) bool {
	r := MatchRule(kind)
	return r != nil && r.Due(createdAt, now)
}
