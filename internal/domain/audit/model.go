package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event is an append-only audit record for sensitive actions.
type Event struct {
	ID         uuid.UUID
	ActorID    *uuid.UUID
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	BranchID   *uuid.UUID
	Metadata   json.RawMessage
	IP         string
	UserAgent  string
	CreatedAt  time.Time
}

type Repository interface {
	Insert(ctx context.Context, e *Event) error
}
