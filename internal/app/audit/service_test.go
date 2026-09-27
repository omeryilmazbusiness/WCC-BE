package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
)

type memRepo struct {
	events    []domain.Event
	insertErr error
	streamed  bool
}

func (m *memRepo) Insert(_ context.Context, e *domain.Event) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.events = append(m.events, *e)
	return nil
}

func (m *memRepo) List(context.Context, domain.ListFilter) ([]domain.Event, int64, error) {
	return m.events, int64(len(m.events)), nil
}

func (m *memRepo) Stream(_ context.Context, _ domain.ListFilter, max int, fn func(domain.Event) error) error {
	m.streamed = true
	for i, e := range m.events {
		if i >= max {
			break
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

func (m *memRepo) ListActions(context.Context) ([]string, error) { return nil, nil }

func TestRecordSeparatesSnapshotsAndUsesContextActor(t *testing.T) {
	repo := &memRepo{}
	svc := NewService(repo)
	user, session := uuid.New(), uuid.New()
	ctx := domain.WithActor(context.Background(), domain.Actor{
		Type: domain.ActorUser, UserID: user, SessionID: session, IP: "10.1.1.1", UserAgent: "ua", RequestID: "req",
	})
	branch := uuid.New()
	err := svc.Record(ctx, domain.RecordInput{
		ActorID: uuid.New(), Action: "booking.updated", EntityType: "booking", BranchID: &branch,
		Before: map[string]any{"a": 1}, After: map[string]any{"a": 2}, Extra: map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := repo.events[0]
	if *e.ActorID != user || e.ActorType != domain.ActorUser || *e.SessionID != session || e.RequestID != "req" || e.IP != "10.1.1.1" {
		t.Fatalf("actor: %+v", e)
	}
	if string(e.Before) != `{"a":1}` || string(e.After) != `{"a":2}` || string(e.Metadata) != `{"k":"v"}` {
		t.Fatalf("payload: before=%s after=%s extra=%s", e.Before, e.After, e.Metadata)
	}
}

func TestRecordWithoutSnapshotsStoresNull(t *testing.T) {
	repo := &memRepo{}
	var none map[string]any
	if err := NewService(repo).Record(context.Background(), domain.RecordInput{Action: "x.y", EntityType: "x", Before: none}); err != nil {
		t.Fatal(err)
	}
	e := repo.events[0]
	if e.Before != nil || e.After != nil || string(e.Metadata) != `{}` || e.ActorType != domain.ActorSystem || e.ActorID != nil {
		t.Fatalf("event: %+v", e)
	}
}

func TestExportIsAuditedFirstAndFailsClosed(t *testing.T) {
	repo := &memRepo{}
	svc := NewService(repo)
	branch := uuid.New()
	var rows int
	if err := svc.Export(context.Background(), domain.ListFilter{Action: "payment.reversed", BranchID: &branch}, func(domain.Event) error {
		rows++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || repo.events[0].Action != "audit.exported" {
		t.Fatalf("export audit first: rows=%d events=%+v", rows, repo.events)
	}
	var extra map[string]any
	_ = json.Unmarshal(repo.events[0].Metadata, &extra)
	if f, ok := extra["filters"].(map[string]any); !ok || f["action"] != "payment.reversed" {
		t.Fatalf("export filters: %v", extra)
	}

	failing := &memRepo{insertErr: errors.New("db down")}
	if err := NewService(failing).Export(context.Background(), domain.ListFilter{}, func(domain.Event) error { return nil }); err == nil || failing.streamed {
		t.Fatal("export must not stream when its audit event cannot be written")
	}
}
