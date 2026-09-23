package inbox

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

const maxEmailBodyRunes = 100_000

// CustomerMatcher locates customers by channel identity (T-102) — ISP.
type CustomerMatcher interface {
	FindByPhone(ctx context.Context, phone string, branchID uuid.UUID) (*MatchedCustomer, error)
	FindByEmail(ctx context.Context, email string, branchID uuid.UUID) (*MatchedCustomer, error)
}

type MatchedCustomer struct {
	ID       uuid.UUID
	FullName string
}

// LeadShellCreator creates an unassigned lead when inbound has no customer (T-103).
type LeadShellCreator interface {
	CreateShell(ctx context.Context, in LeadShellInput) (uuid.UUID, error)
}

type LeadShellInput struct {
	BranchID uuid.UUID
	FullName string
	Phone    string
	Source   string
	OwnerID  uuid.UUID // fallback system/manager owner for shell
	ActorID  uuid.UUID
}

type Service struct {
	repo      domain.Repository
	providers domain.Registry
	customers CustomerMatcher
	leads     LeadShellCreator
	tx        tx.Runner
	bus       *events.Bus
	now       func() time.Time
}

func NewService(repo domain.Repository, providers domain.Registry, txm tx.Runner, bus *events.Bus) *Service {
	return &Service{
		repo: repo, providers: providers, tx: txm, bus: bus,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetCustomerMatcher(m CustomerMatcher) { s.customers = m }
func (s *Service) SetLeadShellCreator(l LeadShellCreator) { s.leads = l }

func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.Conversation, int64, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	return s.repo.ListConversations(ctx, f)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Conversation, error) {
	c, err := s.repo.GetConversation(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, shared.NewNotFound("conversation not found")
	}
	return c, nil
}

func (s *Service) Messages(ctx context.Context, conversationID uuid.UUID, limit int) ([]domain.Message, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if _, err := s.Get(ctx, conversationID); err != nil {
		return nil, err
	}
	return s.repo.ListMessages(ctx, conversationID, limit)
}

type AssignInput struct {
	ConversationID uuid.UUID
	OwnerID        uuid.UUID
	ActorID        uuid.UUID
}

func (s *Service) Assign(ctx context.Context, in AssignInput) (*domain.Conversation, error) {
	if in.OwnerID == uuid.Nil {
		return nil, shared.NewValidation("owner_id is required")
	}
	var out *domain.Conversation
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		c, err := s.repo.GetConversation(ctx, in.ConversationID)
		if err != nil {
			return err
		}
		if c == nil {
			return shared.NewNotFound("conversation not found")
		}
		if c.Status != domain.StatusOpen {
			return shared.NewInvalidState("can only assign open conversations")
		}
		c.Assign(in.OwnerID)
		if err := s.repo.UpdateConversation(ctx, c); err != nil {
			return err
		}
		out = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(ctx, events.Event{Name: events.ConversationAssigned, Payload: out})
	}
	return out, nil
}

type StatusInput struct {
	ConversationID uuid.UUID
	Status         domain.Status
	ActorID        uuid.UUID
}

func (s *Service) SetStatus(ctx context.Context, in StatusInput) (*domain.Conversation, error) {
	var out *domain.Conversation
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		c, err := s.repo.GetConversation(ctx, in.ConversationID)
		if err != nil {
			return err
		}
		if c == nil {
			return shared.NewNotFound("conversation not found")
		}
		switch in.Status {
		case domain.StatusResolved:
			if err := c.MarkResolved(); err != nil {
				return err
			}
		case domain.StatusSpam:
			if err := c.MarkSpam(); err != nil {
				return err
			}
		case domain.StatusDuplicate:
			if err := c.MarkDuplicate(); err != nil {
				return err
			}
		case domain.StatusOpen:
			c.Status = domain.StatusOpen
			c.UpdatedAt = s.now()
			c.SLAStoppedAt = nil
		default:
			return shared.NewValidation("invalid status")
		}
		if err := s.repo.UpdateConversation(ctx, c); err != nil {
			return err
		}
		out = c
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.bus != nil && in.Status == domain.StatusResolved {
		s.bus.Publish(ctx, events.Event{Name: events.ConversationResolved, Payload: out})
	}
	return out, nil
}

type ReplyInput struct {
	ConversationID uuid.UUID
	Body           string
	ActorID        uuid.UUID
	InternalNote   bool
}

func (s *Service) Reply(ctx context.Context, in ReplyInput) (*domain.Message, error) {
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, shared.NewValidation("body is required")
	}
	if utf8.RuneCountInString(body) > maxEmailBodyRunes {
		return nil, shared.NewValidation("body exceeds size limit")
	}

	c, err := s.Get(ctx, in.ConversationID)
	if err != nil {
		return nil, err
	}
	if c.Status != domain.StatusOpen && !in.InternalNote {
		return nil, shared.NewInvalidState("conversation is closed")
	}

	now := s.now()
	msg := &domain.Message{
		ID:             uuid.New(),
		ConversationID: c.ID,
		BranchID:       c.BranchID,
		Body:           body,
		ContentType:    "text/plain",
		Provider:       c.Channel,
		CreatedAt:      now,
	}

	if in.InternalNote {
		msg.Direction = domain.DirectionNote
		msg.Status = domain.MsgNoted
		msg.AuthorUserID = &in.ActorID
		if err := s.repo.InsertMessage(ctx, msg); err != nil {
			return nil, err
		}
		return msg, nil
	}

	msg.Direction = domain.DirectionOut
	msg.AuthorUserID = &in.ActorID
	msg.Status = domain.MsgQueued

	provider, err := s.providers.Get(c.Channel)
	if err != nil {
		return nil, err
	}

	toKey := ""
	if c.ChannelIdentityID != nil {
		identity, ierr := s.repo.GetIdentity(ctx, *c.ChannelIdentityID)
		if ierr != nil {
			return nil, ierr
		}
		if identity != nil {
			toKey = identity.ExternalKey
		}
	}

	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.InsertMessage(ctx, msg); err != nil {
			return err
		}
		res, sendErr := provider.Send(ctx, domain.OutboundRequest{
			Provider:       c.Channel,
			ToExternalKey:  toKey,
			Body:           body,
			ContentType:    "text/plain",
			ConversationID: c.ID.String(),
			IdempotencyKey: msg.ID.String(),
		})
		if sendErr != nil {
			msg.Status = domain.MsgFailed
			msg.ErrorMessage = sendErr.Error()
			if c.IntegrationAccountID != nil {
				_ = s.repo.UpdateAccountHealth(ctx, *c.IntegrationAccountID, "degraded", sendErr.Error(), false)
			}
			return sendErr
		}
		msg.Status = domain.MsgSent
		if res != nil {
			msg.ProviderMessageID = res.ProviderMessageID
		}
		// Persist sent status — Insert already queued; re-insert avoided; status tracked in memory for response.
		c.LastOutboundAt = &now
		c.LastMessagePreview = truncate(body, 140)
		c.StopSLA(now)
		c.UpdatedAt = now
		if c.IntegrationAccountID != nil {
			_ = s.repo.UpdateAccountHealth(ctx, *c.IntegrationAccountID, "ok", "", true)
		}
		return s.repo.UpdateConversation(ctx, c)
	})
	if err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(ctx, events.Event{Name: events.MessageSent, Payload: msg})
	}
	return msg, nil
}

// IngestWebhook handles provider webhooks with dedupe (T-099…T-103).
func (s *Service) IngestWebhook(ctx context.Context, channel domain.Channel, branchID uuid.UUID, headers map[string]string, body []byte) (*domain.Message, error) {
	provider, err := s.providers.Get(channel)
	if err != nil {
		return nil, err
	}
	ev, err := provider.ParseWebhook(ctx, headers, body)
	if err != nil {
		return nil, err
	}
	if ev.ProviderEventID == "" {
		return nil, shared.NewValidation("provider_event_id is required")
	}
	if existing, _ := s.repo.FindMessageByEvent(ctx, ev.Provider, ev.ProviderEventID); existing != nil {
		return existing, nil // idempotent dedupe
	}

	now := s.now()
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = now
	}
	phone := shared.NormalizePhone(ev.Phone)
	email := shared.NormalizeEmail(ev.Email)
	extKey := strings.TrimSpace(ev.ExternalKey)
	if extKey == "" {
		if phone != "" {
			extKey = "wa:" + phone
		} else if email != "" {
			extKey = "email:" + email
		} else {
			return nil, shared.NewValidation("external identity is required")
		}
	}

	acct, _ := s.repo.GetAccount(ctx, branchID, ev.Provider)
	var acctID *uuid.UUID
	if acct != nil {
		acctID = &acct.ID
	}

	identity := &domain.ChannelIdentity{
		ID:          uuid.New(),
		BranchID:    branchID,
		Provider:    ev.Provider,
		ExternalKey: extKey,
		DisplayName: strings.TrimSpace(ev.DisplayName),
		Phone:       phone,
		Email:       email,
		CreatedAt:   now,
	}
	if existing, _ := s.repo.FindIdentity(ctx, branchID, ev.Provider, extKey); existing != nil {
		identity = existing
		if phone != "" {
			identity.Phone = phone
		}
		if email != "" {
			identity.Email = email
		}
		if identity.DisplayName == "" {
			identity.DisplayName = strings.TrimSpace(ev.DisplayName)
		}
	}

	var customerID *uuid.UUID
	if s.customers != nil {
		if phone != "" {
			if cust, _ := s.customers.FindByPhone(ctx, phone, branchID); cust != nil {
				customerID = &cust.ID
				identity.CustomerID = customerID
			}
		}
		if customerID == nil && email != "" {
			if cust, _ := s.customers.FindByEmail(ctx, email, branchID); cust != nil {
				customerID = &cust.ID
				identity.CustomerID = customerID
			}
		}
	}

	var leadID *uuid.UUID
	if customerID == nil && s.leads != nil {
		name := identity.DisplayName
		if name == "" {
			name = "Unknown contact"
		}
		shellPhone := phone
		if shellPhone == "" {
			shellPhone = "+10000000000"
		}
		lid, err := s.leads.CreateShell(ctx, LeadShellInput{
			BranchID: branchID,
			FullName: name,
			Phone:    shellPhone,
			Source:   string(ev.Provider),
			OwnerID:  uuid.Nil, // filled by bridge with default owner
			ActorID:  uuid.Nil,
		})
		if err == nil && lid != uuid.Nil {
			leadID = &lid
		}
	}

	secs, err := s.repo.FirstResponseSeconds(ctx, branchID, ev.Provider)
	if err != nil || secs <= 0 {
		secs = int(domain.DefaultFirstResponse.Seconds())
	}
	window := time.Duration(secs) * time.Second

	var msg *domain.Message
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.UpsertIdentity(ctx, identity); err != nil {
			return err
		}
		conv, _ := s.repo.FindOpenByIdentity(ctx, identity.ID)
		if conv == nil {
			conv = &domain.Conversation{
				ID:                   uuid.New(),
				BranchID:             branchID,
				Channel:              ev.Provider,
				IntegrationAccountID: acctID,
				ChannelIdentityID:    &identity.ID,
				CustomerID:           customerID,
				LeadID:               leadID,
				Subject:              strings.TrimSpace(ev.Subject),
				Status:               domain.StatusOpen,
				CreatedAt:            now,
				UpdatedAt:            now,
			}
			if conv.Subject == "" {
				conv.Subject = truncate(ev.Body, 80)
			}
			conv.StartSLA(now, window)
			conv.LastInboundAt = &now
			conv.UnansweredSince = &now
			conv.LastMessagePreview = truncate(ev.Body, 140)
			if err := s.repo.CreateConversation(ctx, conv); err != nil {
				return err
			}
		} else {
			conv.CustomerID = customerID
			if leadID != nil && conv.LeadID == nil {
				conv.LeadID = leadID
			}
			conv.LastInboundAt = &now
			conv.UnansweredSince = &now
			conv.LastMessagePreview = truncate(ev.Body, 140)
			conv.UpdatedAt = now
			if conv.SLAStoppedAt != nil || conv.LastOutboundAt != nil {
				// Restart SLA after a new inbound following a reply.
				conv.StartSLA(now, window)
			}
			if err := s.repo.UpdateConversation(ctx, conv); err != nil {
				return err
			}
		}

		raw := ev.Raw
		if len(raw) == 0 {
			raw = json.RawMessage(`{}`)
		}
		_ = raw
		msg = &domain.Message{
			ID:                uuid.New(),
			ConversationID:    conv.ID,
			BranchID:          branchID,
			Direction:         domain.DirectionIn,
			Body:              ev.Body,
			ContentType:       defaultCT(ev.ContentType),
			Provider:          ev.Provider,
			ProviderMessageID: ev.ProviderMessageID,
			ProviderEventID:   ev.ProviderEventID,
			Status:            domain.MsgReceived,
			CreatedAt:         ev.OccurredAt,
		}
		if err := s.repo.InsertMessage(ctx, msg); err != nil {
			return err
		}
		if acctID != nil {
			_ = s.repo.UpdateAccountHealth(ctx, *acctID, "ok", "", true)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.bus != nil {
		s.bus.Publish(ctx, events.Event{Name: events.MessageReceived, Payload: msg})
	}
	return msg, nil
}

func (s *Service) CheckSLABreaches(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	now := s.now()
	items, err := s.repo.ListDueForSLABreach(ctx, now, limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range items {
		if err := s.repo.MarkSLABreached(ctx, items[i].ID, now); err != nil {
			return n, err
		}
		n++
		if s.bus != nil {
			s.bus.Publish(ctx, events.Event{Name: events.SLABreached, Payload: &items[i]})
		}
	}
	return n, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func defaultCT(ct string) string {
	if strings.TrimSpace(ct) == "" {
		return "text/plain"
	}
	return ct
}
