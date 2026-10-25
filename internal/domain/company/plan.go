package company

import (
	"strings"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Plan is the commercial WCC package a tenant is billed on.
type Plan string

const (
	PlanStandard Plan = "standard"
	PlanPremium  Plan = "premium"
	PlanUltimate Plan = "ultimate"
)

// LimitKind is a countable resource a plan may cap. Zero on Limits means unlimited.
type LimitKind string

const (
	LimitUsers    LimitKind = "users"
	LimitBranches LimitKind = "branches"
	LimitPackages LimitKind = "packages"
	LimitHotels   LimitKind = "hotels"
	LimitBuses    LimitKind = "buses"
	LimitInbox    LimitKind = "inbox"
)

// Limits are hard caps for one plan. AIPerDay is model answers per user per day.
type Limits struct {
	Users    int
	Branches int
	Packages int
	Hotels   int
	Buses    int
	Inbox    int
	AIPerDay int
}

// Spec is the published entitlement of one plan: allowed permissions and caps.
type Spec struct {
	Plan        Plan
	Permissions []string
	Limits      Limits
}

// AllPlans is the live catalog, cheapest first.
func AllPlans() []Plan {
	return []Plan{PlanStandard, PlanPremium, PlanUltimate}
}

func ParsePlan(s string) (Plan, bool) {
	p := Plan(strings.ToLower(strings.TrimSpace(s)))
	switch p {
	case PlanStandard, PlanPremium, PlanUltimate:
		return p, true
	}
	return "", false
}

func ValidPlan(p Plan) bool {
	_, ok := ParsePlan(string(p))
	return ok
}

func rank(p Plan) int {
	switch Of(p).Plan {
	case PlanStandard:
		return 1
	case PlanPremium:
		return 2
	case PlanUltimate:
		return 3
	default:
		return 1
	}
}

// NextPlan is the next paid tier. Ultimate has nowhere to go.
func NextPlan(p Plan) (Plan, bool) {
	switch Of(p).Plan {
	case PlanStandard:
		return PlanPremium, true
	case PlanPremium:
		return PlanUltimate, true
	default:
		return "", false
	}
}

// CanUpgradeTo is true when `to` is a real plan strictly above `from`.
func CanUpgradeTo(from, to Plan) bool {
	if !ValidPlan(to) {
		return false
	}
	return rank(to) > rank(from)
}

// CheapestPlanFor is the lowest plan that includes perm. Empty if none (platform-only).
func CheapestPlanFor(perm string) (Plan, bool) {
	for _, p := range AllPlans() {
		if Allows(p, perm) {
			return p, true
		}
	}
	return "", false
}

// UpgradePage is the durable key used to dedupe an open upgrade request.
func UpgradePage(to Plan) string {
	return "/upgrade/" + string(to)
}

// Of returns the spec; unknown values fall back to Standard so a missing
// column never grants Ultimate by accident.
func Of(p Plan) Spec {
	switch p {
	case PlanPremium:
		return premiumSpec
	case PlanUltimate:
		return ultimateSpec
	default:
		return standardSpec
	}
}

// Allows reports whether the plan includes a company permission. Platform-only
// permissions (companies.manage, support.manage) are never granted by a plan.
func Allows(p Plan, perm string) bool {
	if perm == "companies.manage" || perm == "support.manage" {
		return false
	}
	if p == PlanUltimate {
		return true
	}
	_, ok := Of(p).allowSet()[perm]
	return ok
}

func (s Spec) allowSet() map[string]struct{} {
	out := make(map[string]struct{}, len(s.Permissions))
	for _, p := range s.Permissions {
		out[p] = struct{}{}
	}
	return out
}

func (l Limits) Cap(kind LimitKind) int {
	switch kind {
	case LimitUsers:
		return l.Users
	case LimitBranches:
		return l.Branches
	case LimitPackages:
		return l.Packages
	case LimitHotels:
		return l.Hotels
	case LimitBuses:
		return l.Buses
	case LimitInbox:
		return l.Inbox
	default:
		return 0
	}
}

// Refuse is nil when used is still under the plan cap (0 = unlimited).
func Refuse(p Plan, kind LimitKind, used int) error {
	max := Of(p).Limits.Cap(kind)
	if max <= 0 || used < max {
		return nil
	}
	err := shared.NewInvalidState("plan limit reached")
	err.Code = "plan_limit"
	err.Details = map[string]any{"kind": string(kind), "max": max, "used": used}
	return err
}

// Standard — 2–3 person starter desk. Core sales/ops, every module capped
// and missing finance, reports, suppliers, visa and AI setup.
var standardSpec = Spec{
	Plan: PlanStandard,
	Limits: Limits{
		Users: 3, Branches: 1, Packages: 8, Hotels: 8, Buses: 3, Inbox: 1, AIPerDay: 50,
	},
	Permissions: []string{
		"users.read", "users.write", "branches.read",
		"customers.read", "customers.write",
		"leads.read", "leads.write",
		"bookings.read", "bookings.write",
		"documents.read", "documents.write",
		"flights.search",
		"packages.read", "packages.write",
		"hotels.read", "hotels.write",
		"buses.read", "buses.write",
		"tasks.read", "tasks.write",
		"inbox.read", "inbox.write",
		"integrations.read", "integrations.write",
		"targets.read",
		"notifications.read", "notifications.write",
		"ai.read", "ai.write",
		"settings.read", "setup.manage",
		"support.write",
	},
}

// Premium — growing agency. Adds money, reports, visa, suppliers, pricing,
// targets write, extra seats and branches. Still no AI setup / FX / override.
var premiumSpec = Spec{
	Plan: PlanPremium,
	Limits: Limits{
		Users: 12, Branches: 3, Packages: 40, Hotels: 40, Buses: 15, Inbox: 3, AIPerDay: 300,
	},
	Permissions: append(append([]string{}, standardSpec.Permissions...),
		"dashboard.read", "users.unlock", "audit.read", "pii.read",
		"bookings.discount",
		"payments.read", "payments.write", "payments.approve",
		"documents.review", "visa.read", "visa.write",
		"suppliers.read", "suppliers.write",
		"hotels.pricing",
		"targets.write",
		"imports.read", "imports.write",
		"notifications.manage",
		"reports.read",
		"settings.write", "branches.manage",
		"ops.read",
	),
}

// Ultimate — full WCC. Permissions are "all company perms" via Allows();
// limits stay unlimited (0).
var ultimateSpec = Spec{
	Plan:   PlanUltimate,
	Limits: Limits{},
}
