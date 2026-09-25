package hardening_test

import (
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/hardening"
)

// TestE2EWorkflowContract simulates the commercial journey stages in order (T-208).
// Full HTTP/DB e2e lives in ops; this locks the acceptance chain in CI without Docker.
func TestE2EWorkflowContract(t *testing.T) {
	seen := map[hardening.WorkflowStage]bool{}
	stages := hardening.CanonicalJourney()

	prev := stages[0]
	seen[prev] = true
	for i := 1; i < len(stages); i++ {
		next := stages[i]
		if !hardening.CanAdvance(prev, next) {
			t.Fatalf("illegal advance %s → %s", prev, next)
		}
		seen[next] = true
		prev = next
	}
	if !hardening.JourneyComplete(seen) {
		t.Fatal("journey incomplete after linear walk")
	}

	// Idempotent stage markers for webhook/import re-delivery semantics (T-209 adjacent).
	key := hardening.NormalizeIdempotencyKey("webhook:inbound:msg-0001")
	if key == "" {
		t.Fatal("webhook idempotency key")
	}
	if !hardening.SameEffect(key, key) {
		t.Fatal("replay must be same effect")
	}
}
