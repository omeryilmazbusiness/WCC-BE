package task_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type noteMem struct {
	mu    sync.Mutex
	items []domain.Note
}

func (m *noteMem) CreateNote(_ context.Context, n *domain.Note) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = append(m.items, *n)
	return nil
}

func (m *noteMem) ListNotes(_ context.Context, taskID uuid.UUID, limit int) ([]domain.Note, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Note
	for _, n := range m.items {
		if n.TaskID == taskID {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (m *noteMem) MarkNotesRead(_ context.Context, taskID, recipientID uuid.UUID, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for i := range m.items {
		it := &m.items[i]
		if it.TaskID == taskID && it.RecipientID == recipientID && it.ReadAt == nil {
			ts := at
			it.ReadAt = &ts
			n++
		}
	}
	return n, nil
}

type outboxMem struct {
	events []events.Event
	fail   error
}

func (o *outboxMem) Add(_ context.Context, ev events.Event) error {
	if o.fail != nil {
		return o.fail
	}
	o.events = append(o.events, ev)
	return nil
}

type rateMem struct{ hits map[string]int }

func (r *rateMem) Hit(_ context.Context, key string, _ time.Duration) (int, error) {
	r.hits[key]++
	return r.hits[key], nil
}

type noteFixture struct {
	svc    *apptask.Service
	notes  *noteMem
	outbox *outboxMem
	rate   *rateMem
	task   *domain.Task
	gm     uuid.UUID
	agent  uuid.UUID
}

func newNoteFixture(t *testing.T) noteFixture {
	t.Helper()
	tasks := newTaskMem()
	gm, agent := uuid.New(), uuid.New()
	task := &domain.Task{
		ID: uuid.New(), BranchID: uuid.New(), Title: "Prepare visa checklist", Kind: domain.KindCustom,
		Status: domain.StatusOpen, AssigneeID: agent, AssigneeName: "Aisha Khan",
		CreatedBy: &gm, CreatedByName: "Omar GM", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := tasks.Create(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	f := noteFixture{
		svc: apptask.NewService(tasks, tx.Nop{}, events.NewBus(nil)), notes: &noteMem{},
		outbox: &outboxMem{}, rate: &rateMem{hits: map[string]int{}}, task: task, gm: gm, agent: agent,
	}
	f.svc.SetNotes(f.notes)
	f.svc.SetOutbox(f.outbox)
	f.svc.SetNoteRateWindow(f.rate)
	return f
}

func as(user uuid.UUID) context.Context {
	return access.WithScope(context.Background(), access.Scope{Level: access.LevelBranch, UserID: user, BranchID: uuid.New()})
}

func TestAssigneeSendsNoteToCreator(t *testing.T) {
	f := newNoteFixture(t)
	n, err := f.svc.AddNote(as(f.agent), f.task.ID, "  Customer needs one more day for the passport scan.  ")
	if err != nil {
		t.Fatal(err)
	}
	if n.AuthorID != f.agent || n.RecipientID != f.gm || n.AuthorName != "Aisha Khan" || n.RecipientName != "Omar GM" {
		t.Fatalf("note addressing = %+v", n)
	}
	if n.Body != "Customer needs one more day for the passport scan." || len(f.notes.items) != 1 {
		t.Fatalf("note not stored trimmed: %+v", f.notes.items)
	}
	if len(f.outbox.events) != 1 || f.outbox.events[0].Name != events.TaskNoteAdded {
		t.Fatalf("event not recorded: %+v", f.outbox.events)
	}
	p, ok := f.outbox.events[0].Payload.(events.TaskNoteAddedPayload)
	if !ok || p.NoteID != n.ID || p.RecipientID != f.gm || p.AuthorName != "Aisha Khan" ||
		p.TaskTitle != f.task.Title || p.BranchID != f.task.BranchID || p.Excerpt != n.Body {
		t.Fatalf("payload = %+v", f.outbox.events[0].Payload)
	}
}

func TestCreatorRepliesToAssignee(t *testing.T) {
	f := newNoteFixture(t)
	n, err := f.svc.AddNote(as(f.gm), f.task.ID, "Fine, update the due date.")
	if err != nil {
		t.Fatal(err)
	}
	if n.RecipientID != f.agent || n.AuthorName != "Omar GM" {
		t.Fatalf("reply addressing = %+v", n)
	}
}

func TestAddNoteRejections(t *testing.T) {
	f := newNoteFixture(t)
	if _, err := f.svc.AddNote(as(uuid.New()), f.task.ID, "hello"); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("outsider: %v", err)
	}
	if _, err := f.svc.AddNote(as(f.agent), uuid.New(), "hello"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown task: %v", err)
	}
	if _, err := f.svc.AddNote(as(f.agent), f.task.ID, "  "); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("empty: %v", err)
	}
	if _, err := f.svc.AddNote(access.WithScope(context.Background(), access.System()), f.task.ID, "hi"); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("system scope has no author: %v", err)
	}
	if _, err := f.svc.AddNote(context.Background(), f.task.ID, "hi"); err == nil {
		t.Fatal("missing scope allowed")
	}
	if len(f.notes.items) != 0 || len(f.outbox.events) != 0 {
		t.Fatalf("rejected notes leaked: %d notes, %d events", len(f.notes.items), len(f.outbox.events))
	}
	if f.rate.hits["task:note:"+f.agent.String()] != 1 {
		t.Fatalf("an invalid body must not spend the rate budget: %+v", f.rate.hits)
	}
}

func TestAddNoteOnClosedTask(t *testing.T) {
	f := newNoteFixture(t)
	if _, err := f.svc.Complete(as(f.agent), f.task.ID, apptask.CompleteInput{Outcome: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddNote(as(f.agent), f.task.ID, "one more thing"); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("closed task: %v", err)
	}
}

func TestAddNoteRateLimited(t *testing.T) {
	f := newNoteFixture(t)
	f.rate.hits["task:note:"+f.agent.String()] = 30
	if _, err := f.svc.AddNote(as(f.agent), f.task.ID, "hello"); !errors.Is(err, shared.ErrRateLimited) {
		t.Fatalf("rate limit: %v", err)
	}
}

func TestAddNoteFailsWhenEventCannotBeRecorded(t *testing.T) {
	f := newNoteFixture(t)
	f.outbox.fail = errors.New("outbox down")
	if _, err := f.svc.AddNote(as(f.agent), f.task.ID, "hello"); err == nil {
		t.Fatal("a note whose alert cannot be recorded must fail")
	}
}

func TestNoteThreadForParticipantsAndOthers(t *testing.T) {
	f := newNoteFixture(t)
	for _, msg := range []string{"first", "second"} {
		if _, err := f.svc.AddNote(as(f.agent), f.task.ID, msg); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.svc.AddNote(as(f.gm), f.task.ID, "reply"); err != nil {
		t.Fatal(err)
	}

	gm, err := f.svc.Notes(as(f.gm), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(gm.Notes) != 3 || gm.Unread != 2 || !gm.Access.CanPost || gm.RecipientName != "Aisha Khan" {
		t.Fatalf("gm thread = %+v", gm)
	}
	agent, err := f.svc.Notes(as(f.agent), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if agent.Unread != 1 || agent.RecipientName != "Omar GM" || agent.Access.RecipientID != f.gm {
		t.Fatalf("agent thread = %+v", agent)
	}
	other, err := f.svc.Notes(as(uuid.New()), f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Notes) != 0 || other.Access.CanRead || other.Access.Blocked != domain.NoteBlockedNotIncluded {
		t.Fatalf("outsider must get the verdict without notes: %+v", other)
	}
}

func TestMarkNotesRead(t *testing.T) {
	f := newNoteFixture(t)
	for _, msg := range []string{"a", "b"} {
		if _, err := f.svc.AddNote(as(f.agent), f.task.ID, msg); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := f.svc.MarkNotesRead(as(f.agent), f.task.ID); err != nil || n != 0 {
		t.Fatalf("author has nothing to mark: n=%d err=%v", n, err)
	}
	if n, err := f.svc.MarkNotesRead(as(f.gm), f.task.ID); err != nil || n != 2 {
		t.Fatalf("recipient marks both: n=%d err=%v", n, err)
	}
	if n, _ := f.svc.MarkNotesRead(as(f.gm), f.task.ID); n != 0 {
		t.Fatalf("second mark must be a no-op: %d", n)
	}
	if th, _ := f.svc.Notes(as(f.gm), f.task.ID); th.Unread != 0 {
		t.Fatalf("unread after mark = %d", th.Unread)
	}
	if _, err := f.svc.MarkNotesRead(as(uuid.New()), f.task.ID); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("outsider mark: %v", err)
	}
}
