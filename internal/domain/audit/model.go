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

type ListFilter struct {
	ActorID    *uuid.UUID
	EntityType string
	EntityID   *uuid.UUID
	Action     string
	BranchID   *uuid.UUID
	From       *time.Time
	To         *time.Time
	Limit      int
	Offset     int
}

type Repository interface {
	Insert(ctx context.Context, e *Event) error
	List(ctx context.Context, f ListFilter) ([]Event, int64, error)
}

// Recorder is the application-facing write port (ISP) used by other services.
type Recorder interface {
	Record(ctx context.Context, in RecordInput) error
}

// RecordInput is the application-facing audit write DTO.
type RecordInput struct {
	ActorID    uuid.UUID
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	BranchID   *uuid.UUID
	Before     any
	After      any
	IP         string
	UserAgent  string
	Extra      map[string]any
}
