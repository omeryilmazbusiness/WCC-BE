package whatsapp

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

// Provider wraps the stub webhook JSON shape with channel=whatsapp.
// Optional Meta-like fields (from, wa_id, text.body) are accepted as aliases.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() domain.Channel { return domain.ChannelWhatsApp }

type metaish struct {
	EventID     string `json:"event_id"`
	MessageID   string `json:"message_id"`
	ExternalKey string `json:"external_key"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Body        string `json:"body"`
	Subject     string `json:"subject"`
	ContentType string `json:"content_type"`
	// Meta-like aliases
	From string `json:"from"`
	WAID string `json:"wa_id"`
	Text *struct {
		Body string `json:"body"`
	} `json:"text"`
}

func (p *Provider) ParseWebhook(_ context.Context, _ map[string]string, body []byte) (*domain.InboundEvent, error) {
	var raw metaish
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, shared.NewValidation("invalid webhook json")
	}
	// Normalize Meta-like fields into stub shape then reuse stub parser.
	if raw.Body == "" && raw.Text != nil {
		raw.Body = raw.Text.Body
	}
	if raw.Phone == "" {
		raw.Phone = firstNonEmpty(raw.From, raw.WAID)
	}
	if raw.ExternalKey == "" && raw.Phone != "" {
		raw.ExternalKey = "wa:" + strings.TrimSpace(raw.Phone)
	}
	if raw.MessageID == "" {
		raw.MessageID = raw.EventID
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
	ev, err := stub.ParseJSON(domain.ChannelWhatsApp, normalized)
	if err != nil {
		return nil, err
	}
	ev.Raw = append(json.RawMessage(nil), body...)
	return ev, nil
}

func (p *Provider) Send(_ context.Context, req domain.OutboundRequest) (*domain.OutboundResult, error) {
	id := fmt.Sprintf("wa-%s", uuid.NewString())
	return &domain.OutboundResult{
		ProviderMessageID: id,
		Raw:               json.RawMessage(fmt.Sprintf(`{"provider_message_id":%q,"to":%q}`, id, req.ToExternalKey)),
	}, nil
}

func (p *Provider) Health(_ context.Context) domain.ProviderHealth {
	return domain.ProviderHealth{
		Provider:  domain.ChannelWhatsApp,
		Status:    "ok",
		LatencyMS: 5,
		Detail:    "whatsapp stub mode",
		CheckedAt: time.Now().UTC(),
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

var _ domain.ChannelProvider = (*Provider)(nil)
