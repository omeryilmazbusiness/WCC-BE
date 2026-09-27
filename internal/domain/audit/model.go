package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Event is an append-only audit record for sensitive actions. Before/After
// hold entity snapshots (nil when not applicable); Metadata holds extra
// context. The table rejects UPDATE/DELETE/TRUNCATE.
type Event struct {
	ID         uuid.UUID
	ActorID    *uuid.UUID
	ActorName  string
	ActorType  ActorType
	Action     string
	EntityType string
	EntityID   *uuid.UUID
	BranchID   *uuid.UUID
	Before     json.RawMessage
	After      json.RawMessage
	Metadata   json.RawMessage
	IP         string
	UserAgent  string
	SessionID  *uuid.UUID
	RequestID  string
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

// ExportMaxRows caps a single CSV export.
const ExportMaxRows = 50000

type Repository interface {
	Insert(ctx context.Context, e *Event) error
	List(ctx context.Context, f ListFilter) ([]Event, int64, error)
	// Stream calls fn for up to max events matching f, newest first.
	Stream(ctx context.Context, f ListFilter, max int, fn func(Event) error) error
	ListActions(ctx context.Context) ([]string, error)
}

// Recorder is the application-facing write port (ISP) used by other services.
type Recorder interface {
	Record(ctx context.Context, in RecordInput) error
}

// RecordInput is the application-facing audit write DTO. ActorID, IP and
// UserAgent are fallbacks: the request actor on ctx takes precedence.
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
