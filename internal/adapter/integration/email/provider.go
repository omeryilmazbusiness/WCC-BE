package email

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/stub"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const maxBodyRunes = 100_000

// Provider accepts stub JSON shape with channel=email.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() domain.Channel { return domain.ChannelEmail }

func (p *Provider) ParseWebhook(_ context.Context, _ map[string]string, body []byte) (*domain.InboundEvent, error) {
	ev, err := stub.ParseJSON(domain.ChannelEmail, body)
	if err != nil {
		return nil, err
	}
	if ev.ExternalKey == "" && ev.Email != "" {
		ev.ExternalKey = "email:" + strings.TrimSpace(ev.Email)
	}
	return ev, nil
}

func (p *Provider) Send(_ context.Context, req domain.OutboundRequest) (*domain.OutboundResult, error) {
	if strings.TrimSpace(req.ToExternalKey) == "" {
		return nil, shared.NewValidation("to_external_key is required")
	}
	if utf8.RuneCountInString(req.Body) > maxBodyRunes {
		return nil, shared.NewValidation("body exceeds size limit")
	}
	id := fmt.Sprintf("email-%s", uuid.NewString())
	return &domain.OutboundResult{
		ProviderMessageID: id,
		Raw:               json.RawMessage(fmt.Sprintf(`{"provider_message_id":%q,"to":%q}`, id, req.ToExternalKey)),
	}, nil
}

func (p *Provider) Health(_ context.Context) domain.ProviderHealth {
	return domain.ProviderHealth{
		Provider:  domain.ChannelEmail,
		Status:    "ok",
		LatencyMS: 8,
		Detail:    "email stub mode",
		CheckedAt: time.Now().UTC(),
	}
}

var _ domain.ChannelProvider = (*Provider)(nil)
