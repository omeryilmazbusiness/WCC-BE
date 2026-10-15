package task

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

// NoteRateWindow counts a key's hits in a sliding window (DIP); nil disables the limit.
type NoteRateWindow interface {
	Hit(ctx context.Context, key string, window time.Duration) (int, error)
}

const (
	notesPerWindow = 30
	noteWindow     = 10 * time.Minute
	// noteExcerptRunes bounds the note text carried into notifications.
	noteExcerptRunes = 140
)

func (s *Service) SetNotes(r domain.NoteRepository)   { s.notes = r }
func (s *Service) SetNoteRateWindow(w NoteRateWindow) { s.noteRate = w }

// NoteThread is a task's notes as one participant sees them.
type NoteThread struct {
	Notes  []domain.Note
	Access domain.NoteAccess
	// RecipientName is who a new note from the viewer would reach.
	RecipientName string
	// Unread counts notes addressed to the viewer they have not opened yet.
	Unread int
}

// Notes returns the thread for the caller. Users who can see the task but do
// not take part get the access verdict without the notes.
func (s *Service) Notes(ctx context.Context, taskID uuid.UUID) (*NoteThread, error) {
	actor, err := noteActor(ctx)
	if err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	acc := t.NoteAccessFor(actor)
	out := &NoteThread{Notes: []domain.Note{}, Access: acc, RecipientName: participantName(t, acc.RecipientID)}
	if !acc.CanRead || s.notes == nil {
		return out, nil
	}
	notes, err := s.notes.ListNotes(ctx, t.ID, domain.MaxNotesPerThread)
	if err != nil {
		return nil, err
	}
	out.Notes = notes
	for i := range notes {
		if notes[i].RecipientID == actor && notes[i].ReadAt == nil {
			out.Unread++
		}
	}
	return out, nil
}

// AddNote posts a note from the caller to the task's other participant and
// records task.note_added in the same transaction, so the recipient's alert
// cannot be lost or sent for a note that was rolled back.
func (s *Service) AddNote(ctx context.Context, taskID uuid.UUID, body string) (*domain.Note, error) {
	if s.notes == nil {
		return nil, shared.NewValidation("task notes are not available")
	}
	actor, err := noteActor(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := domain.NormalizeNoteBody(body); err != nil {
		return nil, err
	}
	if err := s.checkNoteRate(ctx, actor); err != nil {
		return nil, err
	}
	var out *domain.Note
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.repo.FindByID(ctx, taskID)
		if err != nil || t == nil {
			return shared.NewNotFound("task")
		}
		n, err := t.NewNote(actor, body, s.now())
		if err != nil {
			return err
		}
		n.AuthorName = participantName(t, actor)
		n.RecipientName = participantName(t, n.RecipientID)
		if err := s.notes.CreateNote(ctx, n); err != nil {
			return err
		}
		out = n
		return events.Record(ctx, s.outbox, events.Event{Name: events.TaskNoteAdded, Payload: events.TaskNoteAddedPayload{
			NoteID: n.ID, TaskID: t.ID, BranchID: t.BranchID, AuthorID: actor, AuthorName: n.AuthorName,
			RecipientID: n.RecipientID, TaskTitle: t.Title, Excerpt: domain.NoteExcerpt(n.Body, noteExcerptRunes),
		}})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MarkNotesRead stamps the caller's unread notes on a task; it returns how many changed.
func (s *Service) MarkNotesRead(ctx context.Context, taskID uuid.UUID) (int, error) {
	actor, err := noteActor(ctx)
	if err != nil {
		return 0, err
	}
	t, err := s.Get(ctx, taskID)
	if err != nil {
		return 0, err
	}
	if !t.NoteAccessFor(actor).CanRead {
		return 0, shared.NewForbidden("only the assignee and the creator can read notes on this task")
	}
	if s.notes == nil {
		return 0, nil
	}
	return s.notes.MarkNotesRead(ctx, t.ID, actor, s.now())
}

func noteActor(ctx context.Context) (uuid.UUID, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if scope.UserID == uuid.Nil {
		return uuid.Nil, shared.NewForbidden("task notes need a signed-in user")
	}
	return scope.UserID, nil
}

func participantName(t *domain.Task, id uuid.UUID) string {
	switch {
	case id == uuid.Nil:
		return ""
	case id == t.AssigneeID:
		return t.AssigneeName
	case t.CreatedBy != nil && id == *t.CreatedBy:
		return t.CreatedByName
	default:
		return ""
	}
}

func (s *Service) checkNoteRate(ctx context.Context, actor uuid.UUID) error {
	if s.noteRate == nil {
		return nil
	}
	n, err := s.noteRate.Hit(ctx, "task:note:"+actor.String(), noteWindow)
	if err != nil {
		slog.WarnContext(ctx, "task note rate window unavailable", "err", err)
		return nil
	}
	if n > notesPerWindow {
		return shared.NewRateLimited("too many notes; try again in a few minutes", noteWindow)
	}
	return nil
}
