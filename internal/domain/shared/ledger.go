package shared

import "context"

// RunLedger remembers which scheduled deliveries already happened so sweeps
// that run repeatedly act once per (job, scope, period). Claim joins the
// caller's transaction: a rolled-back delivery leaves the slot unclaimed.
type RunLedger interface {
	// Claim returns true when the slot was free and is now taken.
	Claim(ctx context.Context, job, scopeKey, period string) (bool, error)
}
