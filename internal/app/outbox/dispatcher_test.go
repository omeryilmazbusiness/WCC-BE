package outbox

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/realtime"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/outbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

type memRepo struct {
	mu   sync.Mutex
	recs map[uuid.UUID]*domain.Record
}

func newMemRepo() *memRepo { return &memRepo{recs: map[uuid.UUID]*domain.Record{}} }

func (m *memRepo) Insert(_ context.Context, name string, payload []byte, branch *uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := uuid.New()
	m.recs[id] = &domain.Record{ID: id, Name: name, Payload: payload, BranchID: branch, Status: domain.StatusPending}
	return nil
}

func (m *memRepo) Claim(_ context.Context, now time.Time, _ time.Duration, limit int) ([]domain.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Record
	for _, r := range m.recs {
		if len(out) == limit {
			break
		}
		if r.Status == domain.StatusPending && !r.NextAttemptAt.After(now) {
			r.Status = domain.StatusProcessing
			r.Attempts++
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *memRepo) MarkDispatched(_ context.Context, id uuid.UUID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recs[id].Status, m.recs[id].DispatchedAt = domain.StatusDispatched, &at
	return nil
}

func (m *memRepo) MarkFailed(_ context.Context, id uuid.UUID, next time.Time, dead bool, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.recs[id]
	r.Status, r.NextAttemptAt, r.LastError = domain.StatusPending, next, msg
	if dead {
		r.Status = domain.StatusDead
	}
	return nil
}

func (m *memRepo) Stats(context.Context) (domain.Stats, error) { return domain.Stats{}, nil }
func (m *memRepo) ListDead(context.Context, int) ([]domain.Record, error) {
	return nil, nil
}
func (m *memRepo) Requeue(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[id]
	if !ok || r.Status != domain.StatusDead {
		return domain.ErrNotFound
	}
	r.Status, r.Attempts = domain.StatusPending, 0
	return nil
}
func (m *memRepo) PurgeDispatched(context.Context, time.Time) (int64, error) { return 0, nil }

func (m *memRepo) only(t *testing.T) *domain.Record {
	t.Helper()
	if len(m.recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(m.recs))
	}
	for _, r := range m.recs {
		return r
	}
	return nil
}

type announcerFunc func(realtime.Signal)

func (f announcerFunc) Announce(_ context.Context, s realtime.Signal) error { f(s); return nil }

func setup(t *testing.T, handler events.Handler) (*memRepo, *Dispatcher, *time.Time, *[]realtime.Signal) {
	t.Helper()
	repo := newMemRepo()
	bus := events.NewBus(slog.New(slog.NewTextHandler(io.Discard, nil)))
	bus.Subscribe(events.TaskOverdue, handler)
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	var signals []realtime.Signal
	d := NewDispatcher(repo, bus, announcerFunc(func(s realtime.Signal) { signals = append(signals, s) }), Options{
		Now: func() time.Time { return now }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return repo, d, &now, &signals
}

func TestWriterRejectsNonDurableEvents(t *testing.T) {
	w := NewWriter(newMemRepo())
	if err := w.Add(context.Background(), events.Event{Name: events.LeadCreated}); err == nil {
		t.Fatal("in-process events must not be written to the outbox")
	}
}

func TestDispatchDeliversTypedPayloadAndAnnounces(t *testing.T) {
	branch := uuid.New()
	var got events.TaskOverduePayload
	repo, d, _, signals := setup(t, func(_ context.Context, ev events.Event) error {
		got = ev.Payload.(events.TaskOverduePayload)
		return nil
	})
	if err := NewWriter(repo).Add(context.Background(), events.Event{Name: events.TaskOverdue,
		Payload: events.TaskOverduePayload{TaskID: uuid.New(), BranchID: branch, Title: "Call"}}); err != nil {
		t.Fatal(err)
	}
	if n, err := d.DispatchOnce(context.Background()); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got.Title != "Call" || got.BranchID != branch {
		t.Fatalf("payload not delivered: %+v", got)
	}
	if r := repo.only(t); r.Status != domain.StatusDispatched {
		t.Fatalf("status=%s", r.Status)
	}
	if len(*signals) != 1 || (*signals)[0].Type != realtime.TypeInvalidate || *(*signals)[0].BranchID != branch {
		t.Fatalf("signals=%+v", *signals)
	}
}

func TestDispatchRetriesWithBackoffThenDeadLetters(t *testing.T) {
	calls := 0
	repo, d, now, signals := setup(t, func(context.Context, events.Event) error { calls++; return errors.New("db down") })
	_ = NewWriter(repo).Add(context.Background(), events.Event{Name: events.TaskOverdue, Payload: events.TaskOverduePayload{BranchID: uuid.New()}})

	for attempt := 1; attempt <= domain.MaxAttempts; attempt++ {
		if n, _ := d.DispatchOnce(context.Background()); n != 1 {
			t.Fatalf("attempt %d: record not claimed", attempt)
		}
		r := repo.only(t)
		if attempt < domain.MaxAttempts {
			if r.Status != domain.StatusPending || !r.NextAttemptAt.Equal(now.Add(domain.Backoff(attempt))) || r.LastError != "db down" {
				t.Fatalf("attempt %d: %+v", attempt, r)
			}
			if n, _ := d.DispatchOnce(context.Background()); n != 0 {
				t.Fatalf("attempt %d: retried before backoff elapsed", attempt)
			}
			*now = r.NextAttemptAt
		}
	}
	if r := repo.only(t); r.Status != domain.StatusDead {
		t.Fatalf("want dead after %d attempts, got %s", domain.MaxAttempts, r.Status)
	}
	if calls != domain.MaxAttempts || len(*signals) != 0 {
		t.Fatalf("calls=%d signals=%d", calls, len(*signals))
	}
	if err := NewAdmin(repo).Requeue(context.Background(), repo.only(t).ID); err != nil {
		t.Fatal(err)
	}
	if r := repo.only(t); r.Status != domain.StatusPending || r.Attempts != 0 {
		t.Fatalf("requeue: %+v", r)
	}
}

func TestUndecodableRecordIsRetriedNotLost(t *testing.T) {
	repo, d, _, _ := setup(t, func(context.Context, events.Event) error { return nil })
	_ = repo.Insert(context.Background(), events.TaskOverdue, []byte(`{"task_id":42}`), nil)
	_, _ = d.DispatchOnce(context.Background())
	if r := repo.only(t); r.Status != domain.StatusPending || r.LastError == "" {
		t.Fatalf("bad payload must stay for inspection: %+v", r)
	}
}

func TestRequeueUnknownIsNotFound(t *testing.T) {
	err := NewAdmin(newMemRepo()).Requeue(context.Background(), uuid.New())
	var appErr *shared.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("want not-found app error, got %v", err)
	}
}

func TestRunStopsWithContext(t *testing.T) {
	_, d, _, _ := setup(t, func(context.Context, events.Event) error { return nil })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx, 10*time.Millisecond); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not stop")
	}
}
