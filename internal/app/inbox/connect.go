package inbox

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ConnectInput is BYO-token / OAuth-ready credentials per provider (never log secrets).
type ConnectInput struct {
	BranchID    uuid.UUID
	Provider    domain.Channel
	DisplayName string
	// Shared / Meta
	AccessToken string
	// WhatsApp Cloud API
	PhoneNumberID string
	WABAID        string
	DisplayPhone  string
	// Instagram / Facebook Page
	PageID   string
	IGUserID string
	// Gmail
	ClientID     string
	ClientSecret string
	RefreshToken string
	MailboxEmail string
	// Webhook
	VerifyToken string
}

type ConnectResult struct {
	Account    *domain.IntegrationAccount
	WebhookURL string // relative: /v1/webhooks/{provider}?branch_id=
}

func (s *Service) Connect(ctx context.Context, in ConnectInput) (*ConnectResult, error) {
	if !isConnectable(in.Provider) {
		return nil, shared.NewValidation("provider is not connectable")
	}
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	if err := validateConnect(in); err != nil {
		return nil, err
	}

	cfg := buildConfig(in)
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		name = defaultDisplayName(in.Provider)
	}
	verify := strings.TrimSpace(in.VerifyToken)
	if verify == "" {
		verify = uuid.NewString()
		cfg["verify_token"] = verify
		raw, _ = json.Marshal(cfg)
	}

	now := s.now()
	acc := &domain.IntegrationAccount{
		ID:          uuid.New(),
		BranchID:    in.BranchID,
		Provider:    in.Provider,
		DisplayName: name,
		Status:      domain.AccountConnected,
		ConfigJSON:  raw,
		Connected:   true,
		LastOKAt:    &now,
		UpdatedAt:   now,
		PublicMeta:  publicMeta(in.Provider, cfg),
		WebhookPath: "/v1/webhooks/" + string(in.Provider),
	}

	existing, _ := s.repo.GetAccount(ctx, in.BranchID, in.Provider)
	if existing != nil {
		acc.ID = existing.ID
	}
	if err := s.repo.UpsertAccount(ctx, acc); err != nil {
		return nil, err
	}
	acc.ConfigJSON = nil // strip secrets from response path
	return &ConnectResult{
		Account:    acc,
		WebhookURL: acc.WebhookPath + "?branch_id=" + in.BranchID.String(),
	}, nil
}

func (s *Service) Disconnect(ctx context.Context, branchID uuid.UUID, provider domain.Channel) (*domain.IntegrationAccount, error) {
	if !isConnectable(provider) {
		return nil, shared.NewValidation("provider is not connectable")
	}
	acc, err := s.repo.GetAccount(ctx, branchID, provider)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		acc = &domain.IntegrationAccount{
			ID: uuid.New(), BranchID: branchID, Provider: provider,
			DisplayName: defaultDisplayName(provider),
		}
	}
	now := s.now()
	acc.Status = domain.AccountDisconnected
	acc.Connected = false
	acc.ConfigJSON = []byte(`{}`)
	acc.PublicMeta = map[string]string{}
	acc.LastError = ""
	acc.LastOKAt = nil
	acc.UpdatedAt = now
	acc.WebhookPath = "/v1/webhooks/" + string(provider)
	if err := s.repo.UpsertAccount(ctx, acc); err != nil {
		return nil, err
	}
	acc.ConfigJSON = nil
	return acc, nil
}

func (s *Service) IntegrationHealth(ctx context.Context, branchID uuid.UUID) ([]domain.IntegrationAccount, []domain.ProviderHealth, error) {
	accounts, err := s.repo.ListAccounts(ctx, branchID)
	if err != nil {
		return nil, nil, err
	}
	// Ensure all connectable channels appear (disconnected placeholders).
	by := map[domain.Channel]*domain.IntegrationAccount{}
	for i := range accounts {
		a := &accounts[i]
		enrichAccount(a)
		by[a.Provider] = a
	}
	var out []domain.IntegrationAccount
	for _, ch := range domain.ConnectableChannels() {
		if a, ok := by[ch]; ok {
			out = append(out, *a)
			continue
		}
		out = append(out, domain.IntegrationAccount{
			BranchID: branchID, Provider: ch, DisplayName: defaultDisplayName(ch),
			Status: domain.AccountDisconnected, Connected: false,
			WebhookPath: "/v1/webhooks/" + string(ch),
			PublicMeta:  map[string]string{},
		})
	}
	var live []domain.ProviderHealth
	if s.providers != nil {
		for _, p := range s.providers.All() {
			live = append(live, p.Health(ctx))
		}
	}
	return out, live, nil
}

// AccountPublic is a secrets-stripped integration account for list APIs (T-218).
type AccountPublic struct {
	ID          uuid.UUID         `json:"id,omitempty"`
	BranchID    uuid.UUID         `json:"branch_id"`
	Provider    domain.Channel    `json:"provider"`
	DisplayName string            `json:"display_name"`
	Status      string            `json:"status"`
	Connected   bool              `json:"connected"`
	PublicMeta  map[string]string `json:"public_meta"`
	WebhookPath string            `json:"webhook_path"`
	WebhookURL  string            `json:"webhook_url"`
	LastOKAt    *time.Time        `json:"last_ok_at,omitempty"`
	LastError   string            `json:"last_error,omitempty"`
}

func (s *Service) ListAccounts(ctx context.Context, branchID uuid.UUID) ([]AccountPublic, error) {
	accounts, _, err := s.IntegrationHealth(ctx, branchID)
	if err != nil {
		return nil, err
	}
	out := make([]AccountPublic, 0, len(accounts))
	for i := range accounts {
		a := &accounts[i]
		path := a.WebhookPath
		if path == "" {
			path = "/v1/webhooks/" + string(a.Provider)
		}
		out = append(out, AccountPublic{
			ID: a.ID, BranchID: a.BranchID, Provider: a.Provider, DisplayName: a.DisplayName,
			Status: a.Status, Connected: a.Connected, PublicMeta: a.PublicMeta,
			WebhookPath: path, WebhookURL: path + "?branch_id=" + branchID.String(),
			LastOKAt: a.LastOKAt, LastError: a.LastError,
		})
	}
	return out, nil
}

func isConnectable(c domain.Channel) bool {
	for _, x := range domain.ConnectableChannels() {
		if x == c {
			return true
		}
	}
	return false
}

func validateConnect(in ConnectInput) error {
	switch in.Provider {
	case domain.ChannelWhatsApp:
		if strings.TrimSpace(in.AccessToken) == "" || strings.TrimSpace(in.PhoneNumberID) == "" {
			return shared.NewValidation("whatsapp requires access_token and phone_number_id")
		}
	case domain.ChannelInstagram:
		if strings.TrimSpace(in.AccessToken) == "" || strings.TrimSpace(in.PageID) == "" || strings.TrimSpace(in.IGUserID) == "" {
			return shared.NewValidation("instagram requires access_token, page_id, and ig_user_id")
		}
	case domain.ChannelFacebook:
		if strings.TrimSpace(in.AccessToken) == "" || strings.TrimSpace(in.PageID) == "" {
			return shared.NewValidation("facebook requires access_token and page_id")
		}
	case domain.ChannelGmail:
		if strings.TrimSpace(in.ClientID) == "" || strings.TrimSpace(in.ClientSecret) == "" ||
			strings.TrimSpace(in.RefreshToken) == "" || strings.TrimSpace(in.MailboxEmail) == "" {
			return shared.NewValidation("gmail requires client_id, client_secret, refresh_token, and mailbox_email")
		}
	default:
		return shared.NewValidation("unsupported provider")
	}
	return nil
}

func buildConfig(in ConnectInput) map[string]string {
	m := map[string]string{}
	put := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			m[k] = strings.TrimSpace(v)
		}
	}
	put("access_token", in.AccessToken)
	put("phone_number_id", in.PhoneNumberID)
	put("waba_id", in.WABAID)
	put("display_phone", in.DisplayPhone)
	put("page_id", in.PageID)
	put("ig_user_id", in.IGUserID)
	put("client_id", in.ClientID)
	put("client_secret", in.ClientSecret)
	put("refresh_token", in.RefreshToken)
	put("mailbox_email", in.MailboxEmail)
	put("verify_token", in.VerifyToken)
	return m
}

func publicMeta(provider domain.Channel, cfg map[string]string) map[string]string {
	out := map[string]string{}
	copyKeys := []string{"phone_number_id", "waba_id", "display_phone", "page_id", "ig_user_id", "mailbox_email"}
	for _, k := range copyKeys {
		if v := cfg[k]; v != "" {
			out[k] = v
		}
	}
	if tok := cfg["access_token"]; len(tok) > 4 {
		out["access_token_hint"] = "••••" + tok[len(tok)-4:]
	}
	if cfg["refresh_token"] != "" {
		out["has_refresh_token"] = "true"
	}
	if cfg["verify_token"] != "" {
		out["verify_token"] = cfg["verify_token"]
	}
	out["provider"] = string(provider)
	return out
}

func enrichAccount(a *domain.IntegrationAccount) {
	a.Connected = a.Status == domain.AccountConnected || a.Status == domain.AccountOK || a.Status == domain.AccountDegraded
	if len(a.ConfigJSON) > 0 && string(a.ConfigJSON) != "{}" && string(a.ConfigJSON) != "null" {
		var cfg map[string]string
		if json.Unmarshal(a.ConfigJSON, &cfg) == nil {
			if cfg["access_token"] != "" || cfg["refresh_token"] != "" {
				a.Connected = true
				if a.Status == domain.AccountDisconnected || a.Status == "" {
					a.Status = domain.AccountConnected
				}
			}
			a.PublicMeta = publicMeta(a.Provider, cfg)
		}
	}
	if a.PublicMeta == nil {
		a.PublicMeta = map[string]string{}
	}
	a.WebhookPath = "/v1/webhooks/" + string(a.Provider)
	a.ConfigJSON = nil
}

func defaultDisplayName(p domain.Channel) string {
	switch p {
	case domain.ChannelWhatsApp:
		return "WhatsApp Business"
	case domain.ChannelInstagram:
		return "Instagram"
	case domain.ChannelFacebook:
		return "Facebook Messenger"
	case domain.ChannelGmail:
		return "Gmail"
	default:
		return string(p)
	}
}
