package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/stub"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Provider accepts the stub JSON shape with channel=instagram.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() domain.Channel { return domain.ChannelInstagram }

type payload struct {
	EventID     string `json:"event_id"`
	MessageID   string `json:"message_id"`
	ExternalKey string `json:"external_key"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Body        string `json:"body"`
	Subject     string `json:"subject"`
	ContentType string `json:"content_type"`
	SenderID    string `json:"sender_id"`
}

func (p *Provider) ParseWebhook(_ context.Context, _ map[string]string, body []byte) (*domain.InboundEvent, error) {
	var raw payload
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, shared.NewValidation("invalid webhook json")
	}
	if raw.ExternalKey == "" && strings.TrimSpace(raw.SenderID) != "" {
		raw.ExternalKey = "ig:" + strings.TrimSpace(raw.SenderID)
	}
	normalized, err := json.Marshal(map[string]any{
		"event_id":     raw.EventID,
		"message_id":   raw.MessageID,
		"external_key": raw.ExternalKey,
		"display_name": raw.DisplayName,
		"phone":        raw.Phone,
		"email":        raw.Email,
		"body":         raw.Body,
		"subject":      raw.Subject,
		"content_type": raw.ContentType,
	})
	if err != nil {
		return nil, err
	}
	ev, err := stub.ParseJSON(domain.ChannelInstagram, normalized)
	if err != nil {
		return nil, err
	}
	ev.Raw = append(json.RawMessage(nil), body...)
	return ev, nil
}

func (p *Provider) Send(_ context.Context, req domain.OutboundRequest) (*domain.OutboundResult, error) {
	id := fmt.Sprintf("ig-%s", uuid.NewString())
	return &domain.OutboundResult{
		ProviderMessageID: id,
		Raw:               json.RawMessage(fmt.Sprintf(`{"provider_message_id":%q,"to":%q}`, id, req.ToExternalKey)),
	}, nil
}

func (p *Provider) Health(_ context.Context) domain.ProviderHealth {
	return domain.ProviderHealth{
		Provider:  domain.ChannelInstagram,
		Status:    "degraded",
		LatencyMS: 120,
		Detail:    "Meta rate limit (demo)",
		CheckedAt: time.Now().UTC(),
	}
}

var _ domain.ChannelProvider = (*Provider)(nil)
