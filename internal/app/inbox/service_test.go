package inbox_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/stub"
	"github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
)

type memRepo struct {
	mu       sync.Mutex
	idents   map[string]*domain.ChannelIdentity
	identsBy map[uuid.UUID]*domain.ChannelIdentity
	convs    map[uuid.UUID]*domain.Conversation
	msgs     map[uuid.UUID]*domain.Message
	byEvent  map[string]*domain.Message
	accounts []domain.IntegrationAccount
}

func newMem() *memRepo {
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	aid := uuid.New()
	return &memRepo{
		idents:   map[string]*domain.ChannelIdentity{},
		identsBy: map[uuid.UUID]*domain.ChannelIdentity{},
		convs:    map[uuid.UUID]*domain.Conversation{},
		msgs:     map[uuid.UUID]*domain.Message{},
		byEvent:  map[string]*domain.Message{},
		accounts: []domain.IntegrationAccount{{
			ID: aid, BranchID: branch, Provider: domain.ChannelStub, DisplayName: "stub", Status: "ok",
		}},
	}
}

func (m *memRepo) key(b uuid.UUID, p domain.Channel, e string) string {
	return b.String() + "|" + string(p) + "|" + e
}

func (m *memRepo) UpsertIdentity(_ context.Context, id *domain.ChannelIdentity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := m.key(id.BranchID, id.Provider, id.ExternalKey)
	if ex, ok := m.idents[k]; ok {
		*id = *ex
		return nil
	}
	m.idents[k] = id
	m.identsBy[id.ID] = id
	return nil
}
func (m *memRepo) FindIdentity(_ context.Context, branchID uuid.UUID, provider domain.Channel, externalKey string) (*domain.ChannelIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idents[m.key(branchID, provider, externalKey)], nil
}
func (m *memRepo) GetIdentity(_ context.Context, id uuid.UUID) (*domain.ChannelIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.identsBy[id], nil
}
func (m *memRepo) GetAccount(_ context.Context, branchID uuid.UUID, provider domain.Channel) (*domain.IntegrationAccount, error) {
	for i := range m.accounts {
		if m.accounts[i].BranchID == branchID && m.accounts[i].Provider == provider {
			cp := m.accounts[i]
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) ListAccounts(_ context.Context, branchID uuid.UUID) ([]domain.IntegrationAccount, error) {
	var out []domain.IntegrationAccount
	for _, a := range m.accounts {
		if a.BranchID == branchID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memRepo) UpsertAccount(_ context.Context, a *domain.IntegrationAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.accounts {
		if m.accounts[i].BranchID == a.BranchID && m.accounts[i].Provider == a.Provider {
			m.accounts[i] = *a
			return nil
		}
	}
	m.accounts = append(m.accounts, *a)
	return nil
}
func (m *memRepo) UpdateAccountHealth(context.Context, uuid.UUID, string, string, bool) error {
	return nil
}
func (m *memRepo) CreateConversation(_ context.Context, c *domain.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	m.convs[c.ID] = &cp
	return nil
}
func (m *memRepo) UpdateConversation(_ context.Context, c *domain.Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	m.convs[c.ID] = &cp
	return nil
}
func (m *memRepo) GetConversation(_ context.Context, id uuid.UUID) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.convs[id]
	if c == nil {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}
func (m *memRepo) FindOpenByIdentity(_ context.Context, identityID uuid.UUID) (*domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.convs {
		if c.ChannelIdentityID != nil && *c.ChannelIdentityID == identityID && c.Status == domain.StatusOpen {
			cp := *c
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) ListConversations(_ context.Context, f domain.ListFilter) ([]domain.Conversation, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Conversation
	for _, c := range m.convs {
		if f.Status != "" && c.Status != f.Status {
			continue
		}
		out = append(out, *c)
	}
	return out, int64(len(out)), nil
}
func (m *memRepo) InsertMessage(_ context.Context, msg *domain.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *msg
	m.msgs[msg.ID] = &cp
	if msg.ProviderEventID != "" {
		m.byEvent[string(msg.Provider)+"|"+msg.ProviderEventID] = &cp
	}
	return nil
}
func (m *memRepo) FindMessageByEvent(_ context.Context, provider domain.Channel, eventID string) (*domain.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byEvent[string(provider)+"|"+eventID], nil
}
func (m *memRepo) ListMessages(_ context.Context, conversationID uuid.UUID, _ int) ([]domain.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Message
	for _, msg := range m.msgs {
		if msg.ConversationID == conversationID {
			out = append(out, *msg)
		}
	}
	return out, nil
}
func (m *memRepo) FirstResponseSeconds(context.Context, uuid.UUID, domain.Channel) (int, error) {
	return 900, nil
}
func (m *memRepo) ListDueForSLA(_ context.Context, stage domain.SLAStage, now time.Time, limit int) ([]domain.Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	pct := domain.DefaultSLABreachPct
	if stage == domain.SLAWarning {
		pct = domain.DefaultSLAWarnPct
	}
	var out []domain.Conversation
	for _, c := range m.convs {
		if c.Status != domain.StatusOpen || c.SLABreachedAt != nil || c.SLAStoppedAt != nil || c.SLAStartedAt == nil || c.SLADueAt == nil {
			continue
		}
		if stage == domain.SLAWarning && c.SLAWarnedAt != nil {
			continue
		}
		if !domain.SLAThresholdAt(*c.SLAStartedAt, *c.SLADueAt, pct).After(now) {
			out = append(out, *c)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
func (m *memRepo) MarkSLAStage(_ context.Context, stage domain.SLAStage, id uuid.UUID, at time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.convs[id]
	if c == nil {
		return false, nil
	}
	field := &c.SLABreachedAt
	if stage == domain.SLAWarning {
		field = &c.SLAWarnedAt
	}
	if *field != nil {
		return false, nil
	}
	*field = &at
	return true, nil
}

type noopTx struct{}

func (noopTx) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

func TestIngestDedupeAndReplyNote(t *testing.T) {
	repo := newMem()
	reg := integration.NewRegistry(stub.New())
	svc := inbox.NewService(repo, reg, noopTx{}, nil)
	branch := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	body := []byte(`{"event_id":"e1","message_id":"m1","phone":"+905551112233","display_name":"A","body":"hi"}`)
	msg1, err := svc.IngestWebhook(context.Background(), domain.ChannelStub, branch, nil, body)
	if err != nil {
		t.Fatal(err)
	}
	msg2, err := svc.IngestWebhook(context.Background(), domain.ChannelStub, branch, nil, body)
	if err != nil {
		t.Fatal(err)
	}
	if msg1.ID != msg2.ID {
		t.Fatalf("expected dedupe, got %s vs %s", msg1.ID, msg2.ID)
	}
	note, err := svc.Reply(context.Background(), inbox.ReplyInput{
		ConversationID: msg1.ConversationID,
		Body:           "internal",
		ActorID:        uuid.New(),
		InternalNote:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if note.Direction != domain.DirectionNote {
		t.Fatalf("want note, got %s", note.Direction)
	}
}

func TestSLABreach(t *testing.T) {
	repo := newMem()
	reg := integration.NewRegistry(stub.New())
	svc := inbox.NewService(repo, reg, noopTx{}, nil)
	started := time.Now().UTC().Add(-2 * time.Hour)
	past := time.Now().UTC().Add(-time.Hour)
	id := uuid.New()
	_ = repo.CreateConversation(context.Background(), &domain.Conversation{
		ID: id, BranchID: uuid.New(), Channel: domain.ChannelStub, Status: domain.StatusOpen, SLAStartedAt: &started, SLADueAt: &past,
	})
	n, err := svc.CheckSLABreaches(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 breach, got %d", n)
	}
	if n, _ := svc.CheckSLABreaches(context.Background(), 10); n != 0 {
		t.Fatalf("breach must be stamped once, got %d", n)
	}
}

func TestSLAWarningThenBreach(t *testing.T) {
	repo := newMem()
	svc := inbox.NewService(repo, integration.NewRegistry(stub.New()), noopTx{}, nil)
	now := time.Now().UTC()
	started := now.Add(-8 * time.Minute) // 80% of a 10 minute window
	due := started.Add(10 * time.Minute)
	id := uuid.New()
	_ = repo.CreateConversation(context.Background(), &domain.Conversation{
		ID: id, BranchID: uuid.New(), Channel: domain.ChannelStub, Status: domain.StatusOpen, SLAStartedAt: &started, SLADueAt: &due,
	})
	warned, breached, err := svc.CheckSLA(context.Background(), 10)
	if err != nil || warned != 1 || breached != 0 {
		t.Fatalf("warned=%d breached=%d err=%v", warned, breached, err)
	}
	if warned, _, _ := svc.CheckSLA(context.Background(), 10); warned != 0 {
		t.Fatalf("warning must fire once, got %d", warned)
	}
}

func TestSLAThresholdAt(t *testing.T) {
	start := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	due := start.Add(20 * time.Minute)
	if got := domain.SLAThresholdAt(start, due, 75); !got.Equal(start.Add(15 * time.Minute)) {
		t.Fatalf("75%%: %s", got)
	}
	if got := domain.SLAThresholdAt(start, due, 150); !got.Equal(start.Add(30 * time.Minute)) {
		t.Fatalf("150%%: %s", got)
	}
}
