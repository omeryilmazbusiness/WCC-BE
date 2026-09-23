package stub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Provider is a ChannelProvider used for local/dev webhooks (Name=stub).
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() domain.Channel { return domain.ChannelStub }

type webhookPayload struct {
	EventID     string `json:"event_id"`
	MessageID   string `json:"message_id"`
	ExternalKey string `json:"external_key"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Body        string `json:"body"`
	Subject     string `json:"subject"`
	ContentType string `json:"content_type"`
}

func (p *Provider) ParseWebhook(_ context.Context, _ map[string]string, body []byte) (*domain.InboundEvent, error) {
	return ParseJSON(domain.ChannelStub, body)
}

// ParseJSON is shared by channel wrappers that accept the same stub JSON shape.
func ParseJSON(channel domain.Channel, body []byte) (*domain.InboundEvent, error) {
	var raw webhookPayload
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, shared.NewValidation("invalid webhook json")
	}
	ct := strings.TrimSpace(raw.ContentType)
	if ct == "" {
		ct = "text/plain"
	}
	return &domain.InboundEvent{
		Provider:          channel,
		ProviderEventID:   strings.TrimSpace(raw.EventID),
		ProviderMessageID: strings.TrimSpace(raw.MessageID),
		ExternalKey:       strings.TrimSpace(raw.ExternalKey),
		DisplayName:       strings.TrimSpace(raw.DisplayName),
		Phone:             strings.TrimSpace(raw.Phone),
		Email:             strings.TrimSpace(raw.Email),
		Body:              raw.Body,
		ContentType:       ct,
		Subject:           strings.TrimSpace(raw.Subject),
		OccurredAt:        time.Now().UTC(),
		Raw:               append(json.RawMessage(nil), body...),
	}, nil
}

func (p *Provider) Send(_ context.Context, req domain.OutboundRequest) (*domain.OutboundResult, error) {
	id := fmt.Sprintf("stub-%s", uuid.NewString())
	return &domain.OutboundResult{
		ProviderMessageID: id,
		Raw:               json.RawMessage(fmt.Sprintf(`{"provider_message_id":%q,"to":%q}`, id, req.ToExternalKey)),
	}, nil
}

func (p *Provider) Health(_ context.Context) domain.ProviderHealth {
	return domain.ProviderHealth{
		Provider:  domain.ChannelStub,
		Status:    "ok",
		LatencyMS: 1,
		Detail:    "stub provider",
		CheckedAt: time.Now().UTC(),
	}
}

// Named wraps the stub parser under a different channel name (facebook/gmail until native adapters ship).
type Named struct {
	channel domain.Channel
	status  string
	detail  string
}

func NewNamed(channel domain.Channel, status, detail string) *Named {
	if status == "" {
		status = "ok"
	}
	return &Named{channel: channel, status: status, detail: detail}
}

func (p *Named) Name() domain.Channel { return p.channel }

func (p *Named) ParseWebhook(_ context.Context, _ map[string]string, body []byte) (*domain.InboundEvent, error) {
	return ParseJSON(p.channel, body)
}

func (p *Named) Send(_ context.Context, req domain.OutboundRequest) (*domain.OutboundResult, error) {
	id := fmt.Sprintf("%s-%s", p.channel, uuid.NewString())
	return &domain.OutboundResult{
		ProviderMessageID: id,
		Raw:               json.RawMessage(fmt.Sprintf(`{"provider_message_id":%q,"to":%q}`, id, req.ToExternalKey)),
	}, nil
}

func (p *Named) Health(_ context.Context) domain.ProviderHealth {
	detail := p.detail
	if detail == "" {
		detail = string(p.channel) + " stub adapter"
	}
	return domain.ProviderHealth{
		Provider:  p.channel,
		Status:    p.status,
		LatencyMS: 1,
		Detail:    detail,
		CheckedAt: time.Now().UTC(),
	}
}

var _ domain.ChannelProvider = (*Provider)(nil)
var _ domain.ChannelProvider = (*Named)(nil)
