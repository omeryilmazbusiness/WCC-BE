package inbox

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Channel string
type Status string
type Direction string
type MessageStatus string

const (
	ChannelWhatsApp  Channel = "whatsapp"
	ChannelInstagram Channel = "instagram"
	ChannelFacebook  Channel = "facebook"
	ChannelGmail     Channel = "gmail"
	ChannelEmail     Channel = "email"
	ChannelStub      Channel = "stub"

	AccountDisconnected = "disconnected"
	AccountPending      = "pending"
	AccountConnected    = "connected"
	AccountOK           = "ok"
	AccountDegraded     = "degraded"
	AccountDown         = "down"

	StatusOpen      Status = "open"
	StatusResolved  Status = "resolved"
	StatusSpam      Status = "spam"
	StatusDuplicate Status = "duplicate"

	DirectionIn   Direction = "in"
	DirectionOut  Direction = "out"
	DirectionNote Direction = "note"

	MsgReceived MessageStatus = "received"
	MsgQueued   MessageStatus = "queued"
	MsgSent     MessageStatus = "sent"
	MsgFailed   MessageStatus = "failed"
	MsgNoted    MessageStatus = "noted"

	// DefaultFirstResponse is used when no sla_policies row exists.
	DefaultFirstResponse = 15 * time.Minute
)

func ValidChannel(c Channel) bool {
	switch c {
	case ChannelWhatsApp, ChannelInstagram, ChannelFacebook, ChannelGmail, ChannelEmail, ChannelStub:
		return true
	default:
		return false
	}
}

func ConnectableChannels() []Channel {
	return []Channel{ChannelWhatsApp, ChannelInstagram, ChannelFacebook, ChannelGmail}
}

type IntegrationAccount struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	Provider     Channel
	DisplayName  string
	Status       string // disconnected|pending|connected|ok|degraded|down
	ConfigJSON   []byte // secrets — never expose raw in list APIs
	PublicMeta   map[string]string
	Connected    bool
	LastOKAt     *time.Time
	LastError    string
	UpdatedAt    time.Time
	WebhookPath  string // relative hint for FE
}

type ChannelIdentity struct {
	ID          uuid.UUID
	BranchID    uuid.UUID
	Provider    Channel
	ExternalKey string
	DisplayName string
	Phone       string
	Email       string
	CustomerID  *uuid.UUID
	CreatedAt   time.Time
}

type Conversation struct {
	ID                   uuid.UUID
	BranchID             uuid.UUID
	Channel              Channel
	IntegrationAccountID *uuid.UUID
	ChannelIdentityID    *uuid.UUID
	CustomerID           *uuid.UUID
	LeadID               *uuid.UUID
	OwnerID              *uuid.UUID
	OwnerName            string
	Subject              string
	Status               Status
	SLAStartedAt         *time.Time
	SLADueAt             *time.Time
	SLABreachedAt        *time.Time
	SLAStoppedAt         *time.Time
	LastInboundAt        *time.Time
	LastOutboundAt       *time.Time
	UnansweredSince      *time.Time
	LastMessagePreview   string
	CustomerName         string
	IdentityLabel        string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type Message struct {
	ID                uuid.UUID
	ConversationID    uuid.UUID
	BranchID          uuid.UUID
	Direction         Direction
	Body              string
	ContentType       string
	Provider          Channel
	ProviderMessageID string
	ProviderEventID   string
	Status            MessageStatus
	DocumentID        *uuid.UUID
	AuthorUserID      *uuid.UUID
	AuthorName        string
	ErrorMessage      string
	CreatedAt         time.Time
}

type ListFilter struct {
	BranchID       *uuid.UUID
	OwnerID        *uuid.UUID
	UnassignedOnly bool
	MineOnly       bool
	Channel        Channel
	Status         Status
	SLABreached    *bool
	UnansweredMin  *time.Duration // unanswered_since older than
	Query          string
	Limit          int
	Offset         int
}

type Repository interface {
	UpsertIdentity(ctx context.Context, id *ChannelIdentity) error
	FindIdentity(ctx context.Context, branchID uuid.UUID, provider Channel, externalKey string) (*ChannelIdentity, error)
	GetIdentity(ctx context.Context, id uuid.UUID) (*ChannelIdentity, error)
	GetAccount(ctx context.Context, branchID uuid.UUID, provider Channel) (*IntegrationAccount, error)
	ListAccounts(ctx context.Context, branchID uuid.UUID) ([]IntegrationAccount, error)
	UpsertAccount(ctx context.Context, a *IntegrationAccount) error
	UpdateAccountHealth(ctx context.Context, id uuid.UUID, status, lastError string, ok bool) error

	CreateConversation(ctx context.Context, c *Conversation) error
	UpdateConversation(ctx context.Context, c *Conversation) error
	GetConversation(ctx context.Context, id uuid.UUID) (*Conversation, error)
	FindOpenByIdentity(ctx context.Context, identityID uuid.UUID) (*Conversation, error)
	ListConversations(ctx context.Context, f ListFilter) ([]Conversation, int64, error)

	InsertMessage(ctx context.Context, m *Message) error
	FindMessageByEvent(ctx context.Context, provider Channel, eventID string) (*Message, error)
	ListMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]Message, error)

	FirstResponseSeconds(ctx context.Context, branchID uuid.UUID, channel Channel) (int, error)
	ListDueForSLABreach(ctx context.Context, now time.Time, limit int) ([]Conversation, error)
	MarkSLABreached(ctx context.Context, id uuid.UUID, at time.Time) error
}

func (c *Conversation) MarkResolved() error {
	if c.Status != StatusOpen {
		return shared.NewInvalidState("conversation is not open")
	}
	c.Status = StatusResolved
	now := time.Now().UTC()
	c.UpdatedAt = now
	c.SLAStoppedAt = &now
	c.UnansweredSince = nil
	return nil
}

func (c *Conversation) MarkSpam() error {
	c.Status = StatusSpam
	now := time.Now().UTC()
	c.UpdatedAt = now
	c.SLAStoppedAt = &now
	c.UnansweredSince = nil
	return nil
}

func (c *Conversation) MarkDuplicate() error {
	c.Status = StatusDuplicate
	now := time.Now().UTC()
	c.UpdatedAt = now
	c.SLAStoppedAt = &now
	c.UnansweredSince = nil
	return nil
}

func (c *Conversation) Assign(ownerID uuid.UUID) {
	c.OwnerID = &ownerID
	c.UpdatedAt = time.Now().UTC()
}

func (c *Conversation) IsSLABreached(now time.Time) bool {
	if c.SLABreachedAt != nil {
		return true
	}
	if c.Status != StatusOpen || c.SLAStoppedAt != nil || c.SLADueAt == nil {
		return false
	}
	return now.After(*c.SLADueAt)
}

func (c *Conversation) StartSLA(now time.Time, window time.Duration) {
	c.SLAStartedAt = &now
	due := now.Add(window)
	c.SLADueAt = &due
	c.SLABreachedAt = nil
	c.SLAStoppedAt = nil
}

func (c *Conversation) StopSLA(now time.Time) {
	c.SLAStoppedAt = &now
	c.UnansweredSince = nil
}
