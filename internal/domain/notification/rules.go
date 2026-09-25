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
	}
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
	if r == nil || r.EscalateAfter <= 0 || len(r.EscalateToRoles) == 0 {
		return false
	}
	return !createdAt.Add(r.EscalateAfter).After(now)
}
