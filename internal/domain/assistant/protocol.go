// Package assistant holds the rules, capabilities and pure answer logic of the
// in-app AI assistant. See docs/AI_ASSISTANT_PROTOCOL.md.
package assistant

import "time"

// ProtocolVersion changes whenever a rule or capability changes.
const ProtocolVersion = "2026-10.2"

// RuleID names a non-negotiable assistant rule; the UI translates it.
type RuleID string

const (
	RuleReadOnly          RuleID = "read_only"
	RuleNoSend            RuleID = "no_send"
	RuleOwnScope          RuleID = "own_scope"
	RuleNoInventedNumbers RuleID = "no_invented_numbers"
	RuleMinimalData       RuleID = "minimal_data"
	RuleFAQFirst          RuleID = "faq_first"
	RuleFixedCapabilities RuleID = "fixed_capabilities"
	RuleNeverBlocked      RuleID = "never_blocked"
	RuleAudited           RuleID = "audited"
	RuleUserLanguage      RuleID = "user_language"
)

// Rules lists every rule in display order.
func Rules() []RuleID {
	return []RuleID{
		RuleReadOnly, RuleNoSend, RuleOwnScope, RuleNoInventedNumbers, RuleMinimalData,
		RuleFAQFirst, RuleFixedCapabilities, RuleNeverBlocked, RuleAudited, RuleUserLanguage,
	}
}

// CapabilityID names one of the fixed things the assistant can do.
type CapabilityID string

const (
	CapOpsSummary    CapabilityID = "ops_summary"
	CapLeadFocus     CapabilityID = "lead_focus"
	CapRevenueStatus CapabilityID = "revenue_status"
	CapMessageDraft  CapabilityID = "message_draft"
	CapAppHelp       CapabilityID = "app_help"
	CapGreeting      CapabilityID = "greeting"
	CapOutOfScope    CapabilityID = "out_of_scope"
)

// Kind groups capabilities by what they return.
type Kind string

const (
	KindData  Kind = "data"  // explains server-computed facts
	KindDraft Kind = "draft" // writes text for the user to review
	KindHelp  Kind = "help"  // how to use the product
	KindRule  Kind = "rule"  // answered by a fixed rule, never by the model
)

// Permission strings mirror platform/auth; a wiring test keeps them in sync.
const (
	PermAIRead        = "ai.read"
	PermAIWrite       = "ai.write"
	PermDashboardRead = "dashboard.read"
	PermLeadsRead     = "leads.read"
	PermTargetsRead   = "targets.read"
)

// Capability is one closed, auditable assistant action.
type Capability struct {
	ID       CapabilityID
	Kind     Kind
	Requires []string
	// NeedsFacts loads scoped server facts before answering.
	NeedsFacts bool
	// History is how many earlier turns reach the model (≤ HistoryTurns). Data
	// answers get none: the facts are the context, and the answer stays cacheable.
	History int
	// MaxOutputTokens caps the model reply; 0 means the model is never used.
	MaxOutputTokens int
	// Task is the one-line instruction added to the shared system prompt.
	Task string
}

// UsesModel reports whether the capability may call the LLM at all.
func (c Capability) UsesModel() bool { return c.MaxOutputTokens > 0 }

// Allowed reports whether a viewer holding `has` permissions may use it.
func (c Capability) Allowed(has func(string) bool) bool {
	for _, p := range c.Requires {
		if !has(p) {
			return false
		}
	}
	return true
}

var catalog = []Capability{
	{
		ID: CapOpsSummary, Kind: KindData, Requires: []string{PermAIRead, PermDashboardRead},
		NeedsFacts: true, MaxOutputTokens: 280,
		Task: "Brief the user on today's operations from the facts: what needs attention first.",
	},
	{
		ID: CapLeadFocus, Kind: KindData, Requires: []string{PermAIRead, PermLeadsRead, PermDashboardRead},
		NeedsFacts: true, MaxOutputTokens: 300,
		Task: "Say which leads and items to focus on, most urgent first, from the attention list.",
	},
	{
		ID: CapRevenueStatus, Kind: KindData, Requires: []string{PermAIRead, PermTargetsRead},
		NeedsFacts: true, MaxOutputTokens: 260,
		Task: "Explain revenue against the active target (pace, gap) using only the target facts.",
	},
	{
		ID: CapMessageDraft, Kind: KindDraft, Requires: []string{PermAIRead, PermAIWrite},
		History: 4, MaxOutputTokens: 400,
		Task: "Write the requested customer message, warm and concise, ready to review. Use [placeholders] for unknown names, dates and prices.",
	},
	{
		ID: CapAppHelp, Kind: KindHelp, Requires: []string{PermAIRead},
		History: 2, MaxOutputTokens: 220,
		Task: "Explain how to do this in the WODI app in a few steps. Screens: " + screenMap + ". If unsure, point to Settings → Help & FAQ.",
	},
	{ID: CapGreeting, Kind: KindRule, Requires: []string{PermAIRead}},
	{ID: CapOutOfScope, Kind: KindRule},
}

// screenMap is the cheapest possible product map for app_help prompts.
const screenMap = "Manager, Pipeline (leads), Inbox, Customers, Bookings, Packages, Flights, Hotels, Suppliers, " +
	"Tasks, Missing documents, Finance, Reports, Targets, Import/Export, Settings (profile, team, roles, audit, help)"

// Capabilities returns the closed catalogue in display order.
func Capabilities() []Capability {
	out := make([]Capability, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup finds a capability by id.
func Lookup(id CapabilityID) (Capability, bool) {
	for _, c := range catalog {
		if c.ID == id {
			return c, true
		}
	}
	return Capability{}, false
}

// Limits of one chat request; the browser enforces the same numbers.
const (
	MaxPromptChars    = 4000
	MaxMessages       = 50
	HistoryTurns      = 4
	HistoryChars      = 1200
	MaxReplyChars     = 6000
	DefaultDailyQuota = 40
	CacheTTL          = 10 * time.Minute
)
