package task

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

// maxNoteRequestBytes leaves room for JSON escaping of a full-length note.
const maxNoteRequestBytes = 64 << 10

type noteRequest struct {
	Body string `json:"body"`
}

func mapNote(n *domain.Note) map[string]any {
	var read any
	if n.ReadAt != nil {
		read = n.ReadAt.UTC().Format(time.RFC3339Nano)
	}
	return map[string]any{
		"id":             n.ID,
		"task_id":        n.TaskID,
		"author_id":      n.AuthorID,
		"author_name":    n.AuthorName,
		"recipient_id":   n.RecipientID,
		"recipient_name": n.RecipientName,
		"body":           n.Body,
		"created_at":     n.CreatedAt.UTC().Format(time.RFC3339Nano),
		"read_at":        read,
	}
}

func taskID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return uuid.Nil, shared.NewValidation("invalid id")
	}
	return id, nil
}

// ListNotes returns the caller's view of a task's note thread.
func (h Handler) ListNotes(w http.ResponseWriter, r *http.Request) {
	id, err := taskID(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	th, err := h.Svc.Notes(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	notes := make([]map[string]any, 0, len(th.Notes))
	for i := range th.Notes {
		notes = append(notes, mapNote(&th.Notes[i]))
	}
	var recipient any
	if th.Access.RecipientID != uuid.Nil {
		recipient = th.Access.RecipientID
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"notes":          notes,
		"can_read":       th.Access.CanRead,
		"can_post":       th.Access.CanPost,
		"blocked":        th.Access.Blocked,
		"recipient_id":   recipient,
		"recipient_name": th.RecipientName,
		"unread":         th.Unread,
		"max_length":     domain.MaxNoteLength,
	})
}

// AddNote sends a note to the task's other participant.
func (h Handler) AddNote(w http.ResponseWriter, r *http.Request) {
	id, err := taskID(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var req noteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxNoteRequestBytes)).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	n, err := h.Svc.AddNote(r.Context(), id, req.Body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapNote(n))
}

// MarkNotesRead clears the caller's unread notes on a task.
func (h Handler) MarkNotesRead(w http.ResponseWriter, r *http.Request) {
	id, err := taskID(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	n, err := h.Svc.MarkNotesRead(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"marked": n})
}
