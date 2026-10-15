package task

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// MaxNoteLength bounds a task note (runes); mirrors the DB check.
const MaxNoteLength = 2000

// MaxNotesPerThread caps how many notes one task thread returns.
const MaxNotesPerThread = 200

// Note is a message on a task between its two participants: the assignee
// and the manager who created the task.
type Note struct {
	ID            uuid.UUID
	TaskID        uuid.UUID
	BranchID      uuid.UUID
	AuthorID      uuid.UUID
	AuthorName    string // join enrichment (not persisted)
	RecipientID   uuid.UUID
	RecipientName string // join enrichment (not persisted)
	Body          string
	CreatedAt     time.Time
	ReadAt        *time.Time
}

// Why a user cannot post on a task thread; empty means they can.
const (
	NoteBlockedNoCreator   = "no_creator"
	NoteBlockedSelfTask    = "self_assigned"
	NoteBlockedClosed      = "closed"
	NoteBlockedNotIncluded = "not_participant"
)

// NoteAccess is what a user may do on a task's note thread.
type NoteAccess struct {
	CanRead     bool
	CanPost     bool
	RecipientID uuid.UUID
	Blocked     string
}

// NoteAccessFor decides thread rights for userID. Only the assignee and the
// creator take part; a note always goes to the other one. Automation tasks
// (no creator) and self-assigned tasks have no counterpart, and closed tasks
// keep their thread read-only.
func (t *Task) NoteAccessFor(userID uuid.UUID) NoteAccess {
	if userID == uuid.Nil {
		return NoteAccess{Blocked: NoteBlockedNotIncluded}
	}
	if t.CreatedBy == nil || *t.CreatedBy == uuid.Nil {
		return NoteAccess{CanRead: userID == t.AssigneeID, Blocked: NoteBlockedNoCreator}
	}
	creator := *t.CreatedBy
	if creator == t.AssigneeID {
		return NoteAccess{CanRead: userID == creator, Blocked: NoteBlockedSelfTask}
	}
	var recipient uuid.UUID
	switch userID {
	case t.AssigneeID:
		recipient = creator
	case creator:
		recipient = t.AssigneeID
	default:
		return NoteAccess{Blocked: NoteBlockedNotIncluded}
	}
	if t.Status == StatusDone || t.Status == StatusCancelled {
		return NoteAccess{CanRead: true, RecipientID: recipient, Blocked: NoteBlockedClosed}
	}
	return NoteAccess{CanRead: true, CanPost: true, RecipientID: recipient}
}

// NewNote validates a note from authorID on t and addresses it to the other participant.
func (t *Task) NewNote(authorID uuid.UUID, body string, now time.Time) (*Note, error) {
	text, err := NormalizeNoteBody(body)
	if err != nil {
		return nil, err
	}
	acc := t.NoteAccessFor(authorID)
	if !acc.CanPost {
		return nil, noteBlockedError(acc.Blocked)
	}
	return &Note{
		ID: uuid.New(), TaskID: t.ID, BranchID: t.BranchID,
		AuthorID: authorID, RecipientID: acc.RecipientID,
		Body: text, CreatedAt: now.UTC(),
	}, nil
}

func noteBlockedError(reason string) error {
	switch reason {
	case NoteBlockedClosed:
		return shared.NewInvalidState("notes are closed on a finished task")
	case NoteBlockedNoCreator:
		return shared.NewInvalidState("this task was created automatically; there is no one to send a note to")
	case NoteBlockedSelfTask:
		return shared.NewInvalidState("you created this task for yourself; there is no one to send a note to")
	default:
		return shared.NewForbidden("only the assignee and the creator can send notes on this task")
	}
}

// NormalizeNoteBody trims a note and rejects empty or overlong ones.
func NormalizeNoteBody(raw string) (string, error) {
	body := strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if body == "" {
		return "", shared.NewValidation("note is required")
	}
	if len([]rune(body)) > MaxNoteLength {
		return "", shared.NewValidation("note is too long")
	}
	return body, nil
}

// NoteExcerpt shortens a note for notifications without cutting a rune.
func NoteExcerpt(body string, max int) string {
	flat := strings.Join(strings.Fields(body), " ")
	r := []rune(flat)
	if max <= 0 || len(r) <= max {
		return flat
	}
	return strings.TrimSpace(string(r[:max-1])) + "…"
}

// NoteRepository stores task notes (ISP: separate from task persistence).
type NoteRepository interface {
	CreateNote(ctx context.Context, n *Note) error
	// ListNotes returns a task's notes oldest first, at most limit.
	ListNotes(ctx context.Context, taskID uuid.UUID, limit int) ([]Note, error)
	// MarkNotesRead stamps unread notes addressed to recipientID; returns how many.
	MarkNotesRead(ctx context.Context, taskID, recipientID uuid.UUID, at time.Time) (int, error)
}
