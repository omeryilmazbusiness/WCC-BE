package webhook

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/signature"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestExternalAccountID(t *testing.T) {
	cases := []struct {
		name     string
		provider domain.Channel
		body     string
		want     string
	}{
		{"whatsapp cloud", domain.ChannelWhatsApp,
			`{"object":"whatsapp_business_account","entry":[{"id":"WABA","changes":[{"value":{"metadata":{"phone_number_id":"PN1"},"messages":[{"id":"wamid.1"}]}}]}]}`, "PN1"},
		{"whatsapp flat", domain.ChannelWhatsApp, `{"phone_number_id":"PN2","event_id":"e"}`, "PN2"},
		{"explicit override", domain.ChannelWhatsApp, `{"account_id":"ACC","phone_number_id":"PN2"}`, "ACC"},
		{"instagram entry", domain.ChannelInstagram, `{"object":"instagram","entry":[{"id":"IG1","messaging":[{"recipient":{"id":"IG1"}}]}]}`, "IG1"},
		{"messenger", domain.ChannelFacebook, `{"object":"page","entry":[{"id":"PAGE9"}]}`, "PAGE9"},
		{"email to list", domain.ChannelEmail, `{"to":["Inbox@Wodi.Test"],"event_id":"m1"}`, "inbox@wodi.test"},
		{"gmail pubsub", domain.ChannelGmail,
			`{"message":{"data":"` + base64.StdEncoding.EncodeToString([]byte(`{"emailAddress":"Sales@Wodi.Test","historyId":1}`)) + `","messageId":"ps-1"}}`,
			"sales@wodi.test"},
		{"missing", domain.ChannelWhatsApp, `{"body":"hi"}`, ""},
		{"garbage", domain.ChannelWhatsApp, `not json`, ""},
	}
	for _, c := range cases {
		if got := ExternalAccountID(c.provider, []byte(c.body)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestExternalEventID(t *testing.T) {
	if got := ExternalEventID(domain.ChannelWhatsApp, []byte(`{"entry":[{"changes":[{"value":{"messages":[{"id":"wamid.X"}]}}]}]}`)); got != "wamid.X" {
		t.Fatalf("meta message id: %q", got)
	}
	if got := ExternalEventID(domain.ChannelFacebook, []byte(`{"entry":[{"messaging":[{"message":{"mid":"m.1"}}]}]}`)); got != "m.1" {
		t.Fatalf("messenger mid: %q", got)
	}
	if got := ExternalEventID(domain.ChannelGmail, []byte(`{"message":{"messageId":"ps-9"}}`)); got != "ps-9" {
		t.Fatalf("pubsub id: %q", got)
	}
	a := ExternalEventID(domain.ChannelStub, []byte(`{"x":1}`))
	b := ExternalEventID(domain.ChannelStub, []byte(`{"x":2}`))
	if a == b || len(a) != len("sha256:")+64 {
		t.Fatalf("content hash fallback: %q %q", a, b)
	}
}

type fakeAccounts struct {
	acct   *domain.IntegrationAccount
	tokens map[string]bool
}

func (f fakeAccounts) FindAccountByExternalID(_ context.Context, _ domain.Channel, id string) (*domain.IntegrationAccount, error) {
	if f.acct != nil && id == "PN1" {
		return f.acct, nil
	}
	return nil, nil
}

func (f fakeAccounts) AccountVerifyTokenExists(_ context.Context, _ domain.Channel, token string) (bool, error) {
	return f.tokens[token], nil
}

type fakeEvents struct {
	rows map[string]*Event
	all  []*Event
	next *time.Time
}

func (f *fakeEvents) Insert(_ context.Context, e *Event) (bool, error) {
	cp := *e
	f.all = append(f.all, &cp)
	if e.Status == StatusRejected {
		return true, nil
	}
	key := string(e.Provider) + "|" + e.ExternalEventID
	if _, ok := f.rows[key]; ok {
		return false, nil
	}
	f.rows[key] = &cp
	return true, nil
}

func (f *fakeEvents) FindByExternalID(_ context.Context, p domain.Channel, id string) (*Event, error) {
	return f.rows[string(p)+"|"+id], nil
}

func (f *fakeEvents) MarkStatus(_ context.Context, id uuid.UUID, status, msg string, _ time.Time) error {
	for _, r := range f.rows {
		if r.ID == id {
			r.Status, r.Error = status, msg
		}
	}
	return nil
}

func (f *fakeEvents) MarkFailed(_ context.Context, id uuid.UUID, msg string, next *time.Time, _ time.Time) error {
	for _, r := range f.rows {
		if r.ID == id {
			r.Status, r.Error = StatusFailed, msg
			r.Attempts++
			f.next = next
		}
	}
	return nil
}

func (f *fakeEvents) ListRetryable(_ context.Context, now time.Time, _ int) ([]Event, error) {
	var out []Event
	for _, r := range f.rows {
		if r.Status == StatusFailed && f.next != nil && !f.next.After(now) {
			out = append(out, *r)
		}
	}
	return out, nil
}

type fakeIngestor struct {
	calls int
	scope access.Scope
	err   error
}

func (f *fakeIngestor) IngestWebhook(ctx context.Context, _ domain.Channel, branchID uuid.UUID, _ map[string]string, _ []byte) (*domain.Message, error) {
	f.calls++
	f.scope = access.From(ctx)
	if f.err != nil {
		return nil, f.err
	}
	return &domain.Message{ID: uuid.New(), BranchID: branchID}, nil
}

type fakeAudit struct{ actions []string }

func (f *fakeAudit) Record(_ context.Context, in audit.RecordInput) error {
	f.actions = append(f.actions, in.Action)
	return nil
}

const waBody = `{"object":"whatsapp_business_account","entry":[{"changes":[{"value":{"metadata":{"phone_number_id":"PN1"},"messages":[{"id":"wamid.1"}]}}]}]}`

func newTestService(allowUnsigned bool, cfg string) (*Service, *fakeEvents, *fakeIngestor, *fakeAudit, uuid.UUID) {
	branch := uuid.New()
	acct := &domain.IntegrationAccount{ID: uuid.New(), BranchID: branch, Provider: domain.ChannelWhatsApp, ConfigJSON: []byte(cfg)}
	ev := &fakeEvents{rows: map[string]*Event{}}
	ing := &fakeIngestor{}
	au := &fakeAudit{}
	svc := NewService(Deps{
		Accounts: fakeAccounts{acct: acct, tokens: map[string]bool{"vt-1": true}},
		Events:   ev, Ingestor: ing, Audit: au,
		Verifiers: map[domain.Channel]SignatureVerifier{domain.ChannelWhatsApp: signature.Meta()},
		Options:   Options{AllowUnsigned: allowUnsigned},
	})
	return svc, ev, ing, au, branch
}

func TestHandleSignedRoutesToAccountBranch(t *testing.T) {
	svc, ev, ing, _, branch := newTestService(false, `{"app_secret":"s3"}`)
	body := []byte(waBody)
	res, err := svc.Handle(context.Background(), Request{
		Provider: "WhatsApp", Body: body,
		Headers: map[string]string{signature.MetaHeader: signature.SignHeader(body, "s3")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ing.calls != 1 || ing.scope.Level != access.LevelBranch || ing.scope.BranchID != branch {
		t.Fatalf("ingest scope %+v calls %d", ing.scope, ing.calls)
	}
	stored := ev.rows["whatsapp|wamid.1"]
	if stored == nil || stored.Status != StatusProcessed || !stored.SignatureValid || *stored.BranchID != branch {
		t.Fatalf("journal row %+v", stored)
	}

	dup, err := svc.Handle(context.Background(), Request{
		Provider: "whatsapp", Body: body,
		Headers: map[string]string{signature.MetaHeader: signature.SignHeader(body, "s3")},
	})
	if err != nil || !dup.Duplicate || dup.EventID != res.EventID || ing.calls != 1 {
		t.Fatalf("duplicate handling: %+v err=%v calls=%d", dup, err, ing.calls)
	}
}

func TestHandleRejectsBadSignature(t *testing.T) {
	svc, ev, ing, au, _ := newTestService(false, `{"app_secret":"s3"}`)
	body := []byte(waBody)
	_, err := svc.Handle(context.Background(), Request{
		Provider: "whatsapp", Body: body,
		Headers: map[string]string{signature.MetaHeader: signature.SignHeader(body, "wrong")},
	})
	if !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("want unauthorized, got %v", err)
	}
	if ing.calls != 0 || len(ev.rows) != 0 || len(ev.all) != 1 || ev.all[0].Status != StatusRejected {
		t.Fatalf("rejected event must be journaled without claiming the id: %+v", ev.all)
	}
	if len(au.actions) != 1 || au.actions[0] != "webhook.signature_rejected" {
		t.Fatalf("audit %v", au.actions)
	}
}

func TestHandleRequiresSecretUnlessLocal(t *testing.T) {
	svc, _, _, _, _ := newTestService(false, `{}`)
	if _, err := svc.Handle(context.Background(), Request{Provider: "whatsapp", Body: []byte(waBody)}); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("production without secret must reject, got %v", err)
	}
	local, ev, ing, _, _ := newTestService(true, `{}`)
	if _, err := local.Handle(context.Background(), Request{Provider: "whatsapp", Body: []byte(waBody)}); err != nil {
		t.Fatalf("local unsigned: %v", err)
	}
	if ing.calls != 1 || ev.rows["whatsapp|wamid.1"].SignatureValid {
		t.Fatal("local unsigned must ingest and record signature_valid=false")
	}
}

func TestHandleUnknownAccountAndProvider(t *testing.T) {
	svc, _, _, _, _ := newTestService(false, `{}`)
	body := []byte(`{"phone_number_id":"OTHER","event_id":"e1"}`)
	if _, err := svc.Handle(context.Background(), Request{Provider: "whatsapp", Body: body}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
	if _, err := svc.Handle(context.Background(), Request{Provider: "telegram", Body: body}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("unknown provider: %v", err)
	}
	if _, err := svc.Handle(context.Background(), Request{Provider: "whatsapp", Body: []byte(`{}`)}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("missing account id: %v", err)
	}
}

func TestHandleMarksFailure(t *testing.T) {
	svc, ev, ing, _, _ := newTestService(true, `{}`)
	ing.err = shared.NewValidation("bad payload")
	if _, err := svc.Handle(context.Background(), Request{Provider: "whatsapp", Body: []byte(waBody)}); err == nil {
		t.Fatal("expected ingest error")
	}
	if ev.rows["whatsapp|wamid.1"].Status != StatusFailed {
		t.Fatal("failed ingest must mark event failed")
	}
	ing.err = nil
	if _, err := svc.Handle(context.Background(), Request{Provider: "whatsapp", Body: []byte(waBody)}); err != nil || ing.calls != 2 {
		t.Fatalf("failed event must be reprocessed on retry: err=%v calls=%d", err, ing.calls)
	}
}

func TestHandshake(t *testing.T) {
	svc, _, _, _, _ := newTestService(false, `{}`)
	ctx := context.Background()
	if got, err := svc.Handshake(ctx, "whatsapp", "subscribe", "vt-1", "123"); err != nil || got != "123" {
		t.Fatalf("valid handshake: %q %v", got, err)
	}
	if _, err := svc.Handshake(ctx, "whatsapp", "subscribe", "nope", "123"); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := svc.Handshake(ctx, "gmail", "subscribe", "vt-1", "123"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("non-meta provider: %v", err)
	}
	svc.opts.EnvVerifyTokens = map[domain.Channel]string{domain.ChannelFacebook: "env-vt"}
	if got, err := svc.Handshake(ctx, "facebook", "subscribe", "env-vt", "c"); err != nil || got != "c" {
		t.Fatalf("env token: %q %v", got, err)
	}
}

func TestRetryAfterBacksOffAndStops(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	transient := errors.New("provider timeout")
	if at, ok := RetryAfter(2, transient, now); !ok || at != now.Add(5*time.Minute) {
		t.Fatalf("second retry after 5m, got %s %v", at, ok)
	}
	if _, ok := RetryAfter(MaxRetries+1, transient, now); ok {
		t.Fatal("retries must stop after MaxRetries")
	}
	if _, ok := RetryAfter(1, shared.NewValidation("bad"), now); ok {
		t.Fatal("permanent errors are never retried")
	}
}

func TestRetryFailedReplaysTransientFailures(t *testing.T) {
	svc, ev, ing, _, branch := newTestService(true, `{}`)
	clock := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return clock }
	ing.err = errors.New("db unavailable")
	if _, err := svc.Handle(context.Background(), Request{Provider: "whatsapp", Body: []byte(waBody)}); err == nil {
		t.Fatal("expected ingest error")
	}
	row := ev.rows["whatsapp|wamid.1"]
	if row.Status != StatusFailed || row.Attempts != 1 || ev.next == nil || !ev.next.Equal(clock.Add(time.Minute)) {
		t.Fatalf("first failure must schedule a 1m retry: %+v next=%v", row, ev.next)
	}
	if n, _, _ := svc.RetryFailed(context.Background(), 10); n != 0 {
		t.Fatal("retry is not due yet")
	}
	clock = clock.Add(2 * time.Minute)
	ing.err = nil
	retried, recovered, err := svc.RetryFailed(context.Background(), 10)
	if err != nil || retried != 1 || recovered != 1 || row.Status != StatusProcessed {
		t.Fatalf("retried=%d recovered=%d err=%v row=%+v", retried, recovered, err, row)
	}
	if ing.scope.BranchID != branch {
		t.Fatalf("replay must run under the event branch, got %+v", ing.scope)
	}
}
