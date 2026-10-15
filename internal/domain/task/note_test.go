package task_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

func noteTask(status domain.Status) (*domain.Task, uuid.UUID, uuid.UUID) {
	gm, agent := uuid.New(), uuid.New()
	return &domain.Task{ID: uuid.New(), BranchID: uuid.New(), Status: status, AssigneeID: agent, CreatedBy: &gm}, gm, agent
}

func TestNoteAccessParticipants(t *testing.T) {
	task, gm, agent := noteTask(domain.StatusOpen)

	a := task.NoteAccessFor(agent)
	if !a.CanRead || !a.CanPost || a.RecipientID != gm || a.Blocked != "" {
		t.Fatalf("assignee must write to the creator: %+v", a)
	}
	c := task.NoteAccessFor(gm)
	if !c.CanRead || !c.CanPost || c.RecipientID != agent {
		t.Fatalf("creator must reply to the assignee: %+v", c)
	}
	o := task.NoteAccessFor(uuid.New())
	if o.CanRead || o.CanPost || o.Blocked != domain.NoteBlockedNotIncluded {
		t.Fatalf("outsider must not take part: %+v", o)
	}
	if n := task.NoteAccessFor(uuid.Nil); n.CanRead || n.CanPost {
		t.Fatalf("anonymous must not take part: %+v", n)
	}
}

func TestNoteAccessWithoutCounterpart(t *testing.T) {
	agent := uuid.New()
	auto := &domain.Task{Status: domain.StatusOpen, AssigneeID: agent}
	if a := auto.NoteAccessFor(agent); a.CanPost || !a.CanRead || a.Blocked != domain.NoteBlockedNoCreator {
		t.Fatalf("automation task has nobody to write to: %+v", a)
	}
	self := &domain.Task{Status: domain.StatusOpen, AssigneeID: agent, CreatedBy: &agent}
	if a := self.NoteAccessFor(agent); a.CanPost || a.Blocked != domain.NoteBlockedSelfTask {
		t.Fatalf("self-assigned task has nobody to write to: %+v", a)
	}
}

func TestNoteAccessClosedIsReadOnly(t *testing.T) {
	for _, st := range []domain.Status{domain.StatusDone, domain.StatusCancelled} {
		task, gm, agent := noteTask(st)
		for _, u := range []uuid.UUID{gm, agent} {
			a := task.NoteAccessFor(u)
			if !a.CanRead || a.CanPost || a.Blocked != domain.NoteBlockedClosed {
				t.Fatalf("%s: thread must stay readable but closed: %+v", st, a)
			}
		}
	}
	task, _, agent := noteTask(domain.StatusInProgress)
	if !task.NoteAccessFor(agent).CanPost {
		t.Fatal("in-progress task must accept notes")
	}
}

func TestNewNote(t *testing.T) {
	task, gm, agent := noteTask(domain.StatusOpen)
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.FixedZone("x", 3*3600))

	n, err := task.NewNote(agent, "  Customer asked for a later date.\r\nPlease advise.  ", now)
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != "Customer asked for a later date.\nPlease advise." {
		t.Fatalf("body not normalised: %q", n.Body)
	}
	if n.AuthorID != agent || n.RecipientID != gm || n.TaskID != task.ID || n.BranchID != task.BranchID {
		t.Fatalf("addressing wrong: %+v", n)
	}
	if n.ID == uuid.Nil || !n.CreatedAt.Equal(now) || n.CreatedAt.Location() != time.UTC || n.ReadAt != nil {
		t.Fatalf("metadata wrong: %+v", n)
	}

	if _, err := task.NewNote(agent, "   ", now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("empty note: got %v", err)
	}
	if _, err := task.NewNote(agent, strings.Repeat("ع", domain.MaxNoteLength+1), now); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("overlong note: got %v", err)
	}
	if _, err := task.NewNote(agent, strings.Repeat("ع", domain.MaxNoteLength), now); err != nil {
		t.Fatalf("max-length note (runes) rejected: %v", err)
	}
	if _, err := task.NewNote(uuid.New(), "hi", now); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("outsider note: got %v", err)
	}
	task.Status = domain.StatusDone
	if _, err := task.NewNote(agent, "hi", now); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("closed task note: got %v", err)
	}
}

func TestNoteExcerpt(t *testing.T) {
	if got := domain.NoteExcerpt("  a\n\n b  ", 10); got != "a b" {
		t.Fatalf("whitespace not flattened: %q", got)
	}
	if got := domain.NoteExcerpt("مرحبا بكم في وودي", 6); got != "مرحبا…" || len([]rune(got)) != 6 {
		t.Fatalf("rune-safe cut failed: %q", got)
	}
	if got := domain.NoteExcerpt("short", 0); got != "short" {
		t.Fatalf("no limit: %q", got)
	}
}
