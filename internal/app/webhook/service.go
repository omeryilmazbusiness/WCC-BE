// Package webhook authenticates, journals and routes inbound provider
// webhooks to the inbox under the owning branch's scope (T-248…T-250).
package webhook

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Options configures env fallbacks. AllowUnsigned must only be set for
// local environments.
type Options struct {
	AllowUnsigned   bool
	EnvSecrets      map[domain.Channel]string
	EnvVerifyTokens map[domain.Channel]string
}

type Deps struct {
	Accounts  AccountLookup
	Events    EventStore
	Ingestor  Ingestor
	Verifiers map[domain.Channel]SignatureVerifier
	Audit     audit.Recorder
	Log       *slog.Logger
	Options   Options
}

type Service struct {
	accounts  AccountLookup
	events    EventStore
	ingestor  Ingestor
	verifiers map[domain.Channel]SignatureVerifier
	audit     audit.Recorder
	log       *slog.Logger
	opts      Options
	now       func() time.Time
}

func NewService(d Deps) *Service {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{
		accounts: d.Accounts, events: d.Events, ingestor: d.Ingestor, verifiers: d.Verifiers,
		audit: d.Audit, log: d.Log, opts: d.Options,
		now: func() time.Time { return time.Now().UTC() },
	}
}

type Request struct {
	Provider string
	Headers  map[string]string
	Body     []byte
	IP       string
}

type Result struct {
	EventID   uuid.UUID
	Duplicate bool
	Message   *domain.Message
}

// accountSecretKeys are config_json keys that may hold the signing secret.
var accountSecretKeys = []string{"webhook_secret", "app_secret"}

func (s *Service) Handle(ctx context.Context, req Request) (*Result, error) {
	channel, err := parseChannel(req.Provider)
	if err != nil {
		return nil, err
	}
	extAccount := ExternalAccountID(channel, req.Body)
	if extAccount == "" {
		s.log.Warn("webhook without account identifier", "provider", channel, "ip", req.IP)
		return nil, shared.NewValidation("account identifier missing from payload")
	}
	acct, err := s.accounts.FindAccountByExternalID(ctx, channel, extAccount)
	if err != nil {
		return nil, err
	}
	if acct == nil {
		s.log.Warn("webhook for unknown integration account", "provider", channel, "external_account_id", extAccount, "ip", req.IP)
		return nil, shared.NewNotFound("integration account")
	}

	ev := &Event{
		ID: uuid.New(), Provider: channel, ExternalEventID: ExternalEventID(channel, req.Body),
		ExternalAccountID: extAccount, IntegrationAccountID: &acct.ID, BranchID: &acct.BranchID,
		Status: StatusReceived, SourceIP: req.IP, Payload: jsonPayload(req.Body), ReceivedAt: s.now(),
	}
	if err := s.verify(channel, acct, req); err != nil {
		s.reject(ctx, ev, acct, err)
		return nil, shared.NewUnauthorized("invalid webhook signature")
	}
	ev.SignatureValid = s.secretFor(channel, acct) != ""

	inserted, err := s.events.Insert(ctx, ev)
	if err != nil {
		return nil, err
	}
	if !inserted {
		existing, err := s.events.FindByExternalID(ctx, channel, ev.ExternalEventID)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.Status == StatusProcessed {
			return &Result{EventID: existing.ID, Duplicate: true}, nil
		}
		if existing != nil {
			ev.ID = existing.ID
		}
	}

	scoped := access.WithScope(ctx, access.ForBranch(acct.BranchID))
	msg, err := s.ingestor.IngestWebhook(scoped, channel, acct.BranchID, req.Headers, req.Body)
	if err != nil {
		_ = s.events.MarkStatus(ctx, ev.ID, StatusFailed, truncate(err.Error(), 1000), s.now())
		return nil, err
	}
	if err := s.events.MarkStatus(ctx, ev.ID, StatusProcessed, "", s.now()); err != nil {
		s.log.Error("webhook status update failed", "event_id", ev.ID, "error", err)
	}
	return &Result{EventID: ev.ID, Message: msg}, nil
}

// Handshake answers the Meta subscription challenge (GET hub.* params).
func (s *Service) Handshake(ctx context.Context, provider, mode, token, challenge string) (string, error) {
	channel, err := parseChannel(provider)
	if err != nil {
		return "", err
	}
	if !isMeta(channel) {
		return "", shared.NewNotFound("webhook handshake")
	}
	if mode != "subscribe" || token == "" || challenge == "" {
		return "", shared.NewForbidden("invalid verification request")
	}
	if env := s.opts.EnvVerifyTokens[channel]; env != "" && subtle.ConstantTimeCompare([]byte(env), []byte(token)) == 1 {
		return challenge, nil
	}
	ok, err := s.accounts.AccountVerifyTokenExists(ctx, channel, token)
	if err != nil {
		return "", err
	}
	if !ok {
		s.log.Warn("webhook verify token mismatch", "provider", channel)
		return "", shared.NewForbidden("verify token mismatch")
	}
	return challenge, nil
}

func (s *Service) verify(channel domain.Channel, acct *domain.IntegrationAccount, req Request) error {
	secret := s.secretFor(channel, acct)
	if secret == "" {
		if s.opts.AllowUnsigned {
			s.log.Warn("accepting unsigned webhook (local only)", "provider", channel)
			return nil
		}
		return errNoSecret
	}
	v, ok := s.verifiers[channel]
	if !ok {
		return errNoVerifier
	}
	return v.Verify(req.Headers, req.Body, secret)
}

func (s *Service) secretFor(channel domain.Channel, acct *domain.IntegrationAccount) string {
	if acct != nil && len(acct.ConfigJSON) > 0 {
		var cfg map[string]any
		if json.Unmarshal(acct.ConfigJSON, &cfg) == nil {
			for _, k := range accountSecretKeys {
				if v, ok := cfg[k].(string); ok && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
	}
	return s.opts.EnvSecrets[channel]
}

func (s *Service) reject(ctx context.Context, ev *Event, acct *domain.IntegrationAccount, cause error) {
	ev.Status = StatusRejected
	ev.Error = cause.Error()
	if _, err := s.events.Insert(ctx, ev); err != nil {
		s.log.Error("webhook rejection not journaled", "provider", ev.Provider, "error", err)
	}
	s.log.Warn("webhook signature rejected", "provider", ev.Provider, "account_id", acct.ID, "ip", ev.SourceIP, "reason", cause.Error())
	accountID, branchID := acct.ID, acct.BranchID
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: uuid.Nil, Action: "webhook.signature_rejected", EntityType: "integration_account",
		EntityID: &accountID, BranchID: &branchID, IP: ev.SourceIP,
		Extra: map[string]any{"provider": ev.Provider, "webhook_event_id": ev.ID, "reason": cause.Error()},
	})
}

type webhookError string

func (e webhookError) Error() string { return string(e) }

const (
	errNoSecret   webhookError = "webhook secret not configured"
	errNoVerifier webhookError = "no signature verifier for provider"
)

func parseChannel(provider string) (domain.Channel, error) {
	c := domain.Channel(strings.ToLower(strings.TrimSpace(provider)))
	if c == "" || !domain.ValidChannel(c) {
		return "", shared.NewValidation("unknown provider")
	}
	return c, nil
}

func isMeta(c domain.Channel) bool {
	return c == domain.ChannelWhatsApp || c == domain.ChannelInstagram || c == domain.ChannelFacebook
}

// jsonPayload stores non-JSON bodies as a JSON string so the column stays valid.
func jsonPayload(body []byte) json.RawMessage {
	if json.Valid(body) {
		return append(json.RawMessage(nil), body...)
	}
	b, _ := json.Marshal(string(body))
	return b
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
