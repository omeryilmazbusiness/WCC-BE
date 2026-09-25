package hardening

// Package hardening holds cross-cutting Definition-of-Done contracts (Epic 17).
// Pure domain only — no I/O. Adapters and app services remain free of this policy.

// WorkflowStage is the canonical commercial journey used in acceptance tests (T-208).
type WorkflowStage string

const (
	StageInboundMessage WorkflowStage = "inbound_message"
	StageLead           WorkflowStage = "lead"
	StageBooking        WorkflowStage = "booking"
	StagePayment        WorkflowStage = "payment"
	StageDocuments      WorkflowStage = "documents"
	StageTargetSnapshot WorkflowStage = "target_snapshot"
	StageReport         WorkflowStage = "report"
)

// CanonicalJourney is the inbound → report acceptance chain (PDF gate).
func CanonicalJourney() []WorkflowStage {
	return []WorkflowStage{
		StageInboundMessage,
		StageLead,
		StageBooking,
		StagePayment,
		StageDocuments,
		StageTargetSnapshot,
		StageReport,
	}
}

// CanAdvance reports whether moving from→to is a valid forward step
// along the canonical journey (or same stage).
func CanAdvance(from, to WorkflowStage) bool {
	if from == to {
		return true
	}
	order := map[WorkflowStage]int{}
	for i, s := range CanonicalJourney() {
		order[s] = i
	}
	a, okA := order[from]
	b, okB := order[to]
	if !okA || !okB {
		return false
	}
	return b == a+1
}

// JourneyComplete is true when all required stages are present (order-insensitive set check).
func JourneyComplete(seen map[WorkflowStage]bool) bool {
	for _, s := range CanonicalJourney() {
		if !seen[s] {
			return false
		}
	}
	return true
}
