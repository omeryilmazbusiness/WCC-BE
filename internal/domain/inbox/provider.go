package inbox

import (
	"context"
	"encoding/json"
	"time"
)

// InboundEvent is the normalized webhook payload (provider-agnostic) — T-098.
type InboundEvent struct {
	Provider          Channel
	ProviderEventID   string
	ProviderMessageID string
	ExternalKey       string
	DisplayName       string
	Phone             string
	Email             string
	Body              string
	ContentType       string
	Subject           string
	BranchHint        string // optional account/phone id for multi-tenant routing
	OccurredAt        time.Time
	Raw               json.RawMessage
}

// OutboundRequest is what adapters send on reply — T-105.
type OutboundRequest struct {
	Provider          Channel
	ToExternalKey     string
	Body              string
	ContentType       string
	ConversationID    string
	IdempotencyKey    string
}

// OutboundResult is returned by ChannelProvider.Send.
type OutboundResult struct {
	ProviderMessageID string
	Raw               json.RawMessage
}

// ProviderHealth is a live check snapshot — T-107.
type ProviderHealth struct {
	Provider  Channel
	Status    string // ok|degraded|down
	LatencyMS int
	Detail    string
	CheckedAt time.Time
}

// ChannelProvider is the DIP port for WhatsApp / Instagram / Email / Stub (T-098…T-101, T-108).
type ChannelProvider interface {
	Name() Channel
	ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (*InboundEvent, error)
	Send(ctx context.Context, req OutboundRequest) (*OutboundResult, error)
	Health(ctx context.Context) ProviderHealth
}

// Registry resolves a provider by channel name.
type Registry interface {
	Get(channel Channel) (ChannelProvider, error)
	All() []ChannelProvider
}
