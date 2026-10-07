package support

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/support"
)

type memRepo struct {
	rows   map[uuid.UUID]*domain.Request
	next   int64
	filter Filter
}

func (m *memRepo) Create(_ context.Context, r *domain.Request) error {
	m.next++
	r.Number = m.next
	cp := *r
	m.rows[r.ID] = &cp
	return nil
}
func (m *memRepo) Get(_ context.Context, id uuid.UUID) (*domain.Request, error) {
	r, ok := m.rows[id]
	if !ok {
		return nil, shared.NewNotFound("support request")
	}
	cp := *r
	return &cp, nil
}
func (m *memRepo) Update(_ context.Context, r *domain.Request) error {
	cp := *r
	m.rows[r.ID] = &cp
	return nil
}
func (m *memRepo) ListByRequester(_ context.Context, id uuid.UUID, _ int) ([]domain.Request, error) {
	var out []domain.Request
	for _, r := range m.rows {
		if r.RequesterID == id {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (m *memRepo) ListInbound(_ context.Context, f Filter) ([]Inbound, int, error) {
	m.filter = f
	var out []Inbound
	for _, r := range m.rows {
		if f.Status == "" || r.Status == f.Status {
			out = append(out, Inbound{Request: *r})
		}
	}
	return out, len(out), nil
}
func (m *memRepo) CountByStatus(context.Context) (map[domain.Status]int, error) {
	c := map[domain.Status]int{}
	for _, r := range m.rows {
		c[r.Status]++
	}
	return c, nil
}

type memAudit struct{ actions []string }

func (a *memAudit) Record(_ context.Context, in audit.RecordInput) error {
	a.actions = append(a.actions, in.Action)
	return nil
}

type window struct{ hits map[string]int }

func (w *window) Hit(_ context.Context, key string, _ time.Duration) (int, error) {
	w.hits[key]++
	return w.hits[key], nil
}

func setup() (*Service, *memRepo, *memAudit) {
	repo := &memRepo{rows: map[uuid.UUID]*domain.Request{}}
	rec := &memAudit{}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	return NewService(repo, rec, &window{hits: map[string]int{}}, Config{Now: func() time.Time { return now }}), repo, rec
}

var (
	user  = Actor{UserID: uuid.New(), BranchID: uuid.New()}
	admin = Actor{UserID: uuid.New()}
	valid = domain.Draft{Title: "Export fails", Description: "Clicking export shows an error.", Page: "/en/reports"}
)

func TestSubmitAndMine(t *testing.T) {
	svc, _, rec := setup()
	r, err := svc.Submit(context.Background(), user, valid)
	if err != nil || r.Number != 1 || r.Status != domain.StatusOpen || r.RequesterID != user.UserID || r.BranchID != user.BranchID {
		t.Fatalf("%v %+v", err, r)
	}
	mine, _ := svc.Mine(context.Background(), user.UserID)
	other, _ := svc.Mine(context.Background(), uuid.New())
	if len(mine) != 1 || len(other) != 0 {
		t.Fatal("users see only their own requests")
	}
	if len(rec.actions) != 1 || rec.actions[0] != "support.request_created" {
		t.Fatalf("audited: %v", rec.actions)
	}
}

func TestSubmitRateLimitedPerUser(t *testing.T) {
	svc, _, _ := setup()
	for i := 0; i < defaultSubmitPerHour; i++ {
		if _, err := svc.Submit(context.Background(), user, valid); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Submit(context.Background(), user, valid); !errors.Is(err, shared.ErrRateLimited) {
		t.Fatalf("want rate limited, got %v", err)
	}
	if _, err := svc.Submit(context.Background(), Actor{UserID: uuid.New(), BranchID: uuid.New()}, valid); err != nil {
		t.Fatal("other users are not affected")
	}
}

func TestInvalidDraftDoesNotSpendRate(t *testing.T) {
	svc, _, _ := setup()
	for i := 0; i < 10; i++ {
		if _, err := svc.Submit(context.Background(), user, domain.Draft{Title: "x"}); !errors.Is(err, shared.ErrValidation) {
			t.Fatal("validation error expected")
		}
	}
	if _, err := svc.Submit(context.Background(), user, valid); err != nil {
		t.Fatal("typos never lock the user out")
	}
}

func TestInboxAndUpdate(t *testing.T) {
	svc, repo, rec := setup()
	r, _ := svc.Submit(context.Background(), user, valid)
	note := "Fixed in today's release"
	got, err := svc.Update(context.Background(), admin, r.ID, domain.StatusResolved, &note)
	if err != nil || got.Status != domain.StatusResolved || got.AdminNote != note || *got.ResolvedBy != admin.UserID {
		t.Fatalf("%v %+v", err, got)
	}
	inbox, err := svc.Inbox(context.Background(), Filter{Status: domain.StatusResolved, Limit: 9999, Offset: -5})
	if err != nil || inbox.Total != 1 || inbox.Counts[domain.StatusResolved] != 1 {
		t.Fatalf("%v %+v", err, inbox)
	}
	if repo.filter.Limit != maxInboxPage || repo.filter.Offset != 0 {
		t.Fatalf("paging clamped: %+v", repo.filter)
	}
	if _, err := svc.Inbox(context.Background(), Filter{Status: "weird"}); !errors.Is(err, shared.ErrValidation) {
		t.Fatal("unknown status rejected")
	}
	if _, err := svc.Update(context.Background(), admin, uuid.New(), domain.StatusOpen, nil); !errors.Is(err, shared.ErrNotFound) {
		t.Fatal("missing request is not found")
	}
	if rec.actions[len(rec.actions)-1] != "support.request_updated" {
		t.Fatal("updates are audited")
	}
}
