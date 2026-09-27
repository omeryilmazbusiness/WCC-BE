package webhook

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
)

// Event statuses stored in webhook_events.status.
const (
	StatusReceived  = "received"
	StatusProcessed = "processed"
	StatusFailed    = "failed"
	StatusRejected  = "rejected"
)

// Event is a journaled inbound webhook delivery.
type Event struct {
	ID                   uuid.UUID
	Provider             domain.Channel
	ExternalEventID      string
	ExternalAccountID    string
	IntegrationAccountID *uuid.UUID
	BranchID             *uuid.UUID
	SignatureValid       bool
	Status               string
	Error                string
	SourceIP             string
	Payload              json.RawMessage
	ReceivedAt           time.Time
	ProcessedAt          *time.Time
}

// SignatureVerifier authenticates a raw webhook body for one provider.
// headers are keyed lowercase.
type SignatureVerifier interface {
	Verify(headers map[string]string, body []byte, secret string) error
}

// AccountLookup resolves provider-side identifiers to integration accounts
// without a caller scope (webhooks arrive before any branch is known).
type AccountLookup interface {
	FindAccountByExternalID(ctx context.Context, provider domain.Channel, externalID string) (*domain.IntegrationAccount, error)
	AccountVerifyTokenExists(ctx context.Context, provider domain.Channel, token string) (bool, error)
}

type EventStore interface {
	// Insert returns false when a non-rejected event with the same
	// (provider, external_event_id) already exists.
	Insert(ctx context.Context, e *Event) (bool, error)
	FindByExternalID(ctx context.Context, provider domain.Channel, externalEventID string) (*Event, error)
	MarkStatus(ctx context.Context, id uuid.UUID, status, errMsg string, at time.Time) error
}

// Ingestor hands a verified, branch-resolved payload to the inbox.
type Ingestor interface {
	IngestWebhook(ctx context.Context, channel domain.Channel, branchID uuid.UUID, headers map[string]string, body []byte) (*domain.Message, error)
}
