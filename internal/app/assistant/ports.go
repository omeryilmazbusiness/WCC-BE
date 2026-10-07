// Package assistant answers in-app assistant questions through a cheapest-first
// pipeline (rules → essential → cache → model). See docs/AI_ASSISTANT_PROTOCOL.md.
package assistant

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/assistant"
)

// FactsReader loads the viewer's scoped facts (dashboard counts, attention, target).
type FactsReader interface {
	Facts(ctx context.Context, branchID uuid.UUID) (*domain.Facts, error)
}

// Completion is one audited model call.
type Completion struct {
	BranchID   uuid.UUID
	ActorID    uuid.UUID
	Capability domain.CapabilityID
	Prompt     domain.Prompt
	InputHash  string
}

// Completer is the branch's configured LLM (BYO provider + key).
type Completer interface {
	Configured(ctx context.Context, branchID uuid.UUID) (bool, error)
	Complete(ctx context.Context, in Completion) (text string, err error)
}

// UsageStore counts a user's model answers per day.
type UsageStore interface {
	Used(ctx context.Context, userID uuid.UUID, day time.Time) (int, error)
	Add(ctx context.Context, userID, branchID uuid.UUID, day time.Time, tokens int) error
}

// AnswerCache keeps recent model answers so repeats cost nothing.
type AnswerCache interface {
	Get(key string) (string, bool)
	Put(key, value string)
}

// RateWindow counts recent events per key (platform/ratelimit satisfies it).
type RateWindow interface {
	Hit(ctx context.Context, key string, window time.Duration) (int, error)
}
