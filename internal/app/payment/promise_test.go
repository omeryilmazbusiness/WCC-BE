package payment

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type taskCall struct {
	kind     string
	promise  uuid.UUID
	assignee uuid.UUID
	dueAt    time.Time
	taskID   uuid.UUID
	done     bool
	outcome  string
}

type tasksMem struct{ calls []taskCall }

func (m *tasksMem) EnsurePromiseFollowUp(_ context.Context, p *domain.Promise, assignee uuid.UUID, dueAt time.Time) (uuid.UUID, error) {
	id := uuid.New()
	m.calls = append(m.calls, taskCall{kind: "follow_up", promise: p.ID, assignee: assignee, dueAt: dueAt, taskID: id})
	return id, nil
}

func (m *tasksMem) EnsurePromiseBroken(_ context.Context, p *domain.Promise, assignee uuid.UUID) error {
	m.calls = append(m.calls, taskCall{kind: "broken", promise: p.ID, assignee: assignee})
	return nil
}

func (m *tasksMem) CloseTask(_ context.Context, id uuid.UUID, done bool, outcome string) error {
	m.calls = append(m.calls, taskCall{kind: "close", taskID: id, done: done, outcome: outcome})
	return nil
}

func (m *tasksMem) kinds() []string {
	out := make([]string, 0, len(m.calls))
	for _, c := range m.calls {
		out = append(out, c.kind)
	}
	return out
}

type notifierMem struct{ owners []uuid.UUID }

func (n *notifierMem) NotifyPromiseBroken(_ context.Context, _ *domain.Promise, owner uuid.UUID) error {
	n.owners = append(n.owners, owner)
	return nil
}

type promiseFixture struct {
	*fixture
	promises *Promises
	tasks    *tasksMem
	notifier *notifierMem
}

func newPromiseFixture(t *testing.T, now time.Time) *promiseFixture {
	f := &promiseFixture{fixture: newFixture(t), tasks: &tasksMem{}, notifier: &notifierMem{}}
	f.promises = NewPromises(f.payments, f.bookings, tx.Nop{}, f.audit, PromiseDeps{
		Tasks: f.tasks, Notifier: f.notifier, Clock: Clock{Now: func() time.Time { return now }},
	})
	return f
}

func (f *promiseFixture) create(t *testing.T, amount int64, on time.Time) *domain.Promise {
	t.Helper()
	p, err := f.promises.Create(context.Background(), CreatePromiseInput{
		BookingID: f.booking.ID, Amount: amount, PromisedOn: on, ActorID: uuid.New(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreatePromiseSchedulesFollowUpForOwner(t *testing.T) {
	f := newPromiseFixture(t, today.Add(10*time.Hour))
	on := today.AddDate(0, 0, 5)
	p := f.create(t, 20000, on)
	if p.Currency != "SAR" || p.Status != domain.PromiseOpen || p.TaskID == nil {
		t.Fatalf("promise = %+v", p)
	}
	c := f.tasks.calls[0]
	if c.kind != "follow_up" || c.assignee != f.booking.OwnerID || c.taskID != *p.TaskID || !c.dueAt.Equal(on.Add(9*time.Hour)) {
		t.Fatalf("follow-up task = %+v", c)
	}
	if got := f.audit.actions(); len(got) != 1 || got[0] != "payment.promise_created" {
		t.Fatalf("audit = %v", got)
	}

	ctx := context.Background()
	for name, in := range map[string]CreatePromiseInput{
		"past date":      {BookingID: f.booking.ID, Amount: 1, PromisedOn: today.AddDate(0, 0, -1)},
		"zero amount":    {BookingID: f.booking.ID, Amount: 0, PromisedOn: on},
		"other currency": {BookingID: f.booking.ID, Amount: 1, Currency: "usd", PromisedOn: on},
	} {
		if _, err := f.promises.Create(ctx, in); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
}

func TestPromiseEffectiveStatusAndSummary(t *testing.T) {
	f := newPromiseFixture(t, today.Add(10*time.Hour))
	kept := f.create(t, 10000, today.AddDate(0, 0, 3))
	open := f.create(t, 50000, today.AddDate(0, 0, 7))
	if _, err := f.record(t, RecordInput{Amount: 12000, AutoVerify: true, MayApprove: true}); err != nil {
		t.Fatal(err)
	}

	items, err := f.promises.List(context.Background(), f.booking.ID)
	if err != nil {
		t.Fatal(err)
	}
	status := map[uuid.UUID]domain.PromiseStatus{}
	for _, p := range items {
		status[p.ID] = p.Status
	}
	if status[kept.ID] != domain.PromiseKept || status[open.ID] != domain.PromiseOpen {
		t.Fatalf("effective statuses = %v", status)
	}
	if f.payments.promises[kept.ID].Status != domain.PromiseOpen {
		t.Fatal("reads must not persist status")
	}

	sum, err := f.promises.Summary(context.Background(), f.booking.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sum.OpenCount != 1 || sum.OpenAmount != 50000 || !sum.NextPromisedOn.Equal(open.PromisedOn) {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestResolveDuePersistsOutcomes(t *testing.T) {
	created := today.Add(10 * time.Hour)
	f := newPromiseFixture(t, created)
	kept := f.create(t, 5000, today)
	broken := f.create(t, 90000, today)
	pending := f.create(t, 90000, today.AddDate(0, 0, 3))
	if _, err := f.record(t, RecordInput{Amount: 6000, AutoVerify: true, MayApprove: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range f.payments.promises {
		p.CreatedAt = created.Add(-time.Hour)
	}

	f.promises.deps.Clock = Clock{Now: func() time.Time { return today.AddDate(0, 0, 1).Add(8 * time.Hour) }}
	f.audit.entries = nil
	f.tasks.calls = nil
	nKept, nBroken, err := f.promises.ResolveDue(context.Background())
	if err != nil || nKept != 1 || nBroken != 1 {
		t.Fatalf("kept=%d broken=%d err=%v", nKept, nBroken, err)
	}
	if f.payments.promises[kept.ID].Status != domain.PromiseKept || f.payments.promises[broken.ID].Status != domain.PromiseBroken ||
		f.payments.promises[pending.ID].Status != domain.PromiseOpen {
		t.Fatal("statuses not persisted")
	}
	if len(f.notifier.owners) != 1 || f.notifier.owners[0] != f.booking.OwnerID {
		t.Fatalf("notifications = %v", f.notifier.owners)
	}
	var closedKept, brokenTask bool
	for _, c := range f.tasks.calls {
		closedKept = closedKept || (c.kind == "close" && c.done && c.outcome == "promise_kept" && c.taskID == *kept.TaskID)
		brokenTask = brokenTask || (c.kind == "broken" && c.promise == broken.ID && c.assignee == f.booking.OwnerID)
	}
	if !closedKept || !brokenTask {
		t.Fatalf("task calls = %v", f.tasks.kinds())
	}
	for _, e := range f.audit.entries {
		if e.ActorID != uuid.Nil {
			t.Fatalf("job audit must be system: %+v", e)
		}
	}

	if nKept, nBroken, _ = f.promises.ResolveDue(context.Background()); nKept+nBroken != 0 {
		t.Fatal("second run must be a no-op")
	}
}

func TestCancelPromise(t *testing.T) {
	f := newPromiseFixture(t, today.Add(10*time.Hour))
	p := f.create(t, 10000, today.AddDate(0, 0, 2))
	got, err := f.promises.Cancel(context.Background(), p.ID, uuid.New())
	if err != nil || got.Status != domain.PromiseCancelled || got.ResolvedAt == nil {
		t.Fatalf("cancel: %+v %v", got, err)
	}
	last := f.tasks.calls[len(f.tasks.calls)-1]
	if last.kind != "close" || last.done || last.outcome != "promise_cancelled" {
		t.Fatalf("task close = %+v", last)
	}
	if _, err := f.promises.Cancel(context.Background(), p.ID, uuid.New()); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("second cancel: %v", err)
	}
}
