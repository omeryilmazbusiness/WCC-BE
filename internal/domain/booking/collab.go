package booking

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const MaxNoteLen = 4000

// Note is an internal staff note on a booking; customers never see it.
type Note struct {
	ID         uuid.UUID
	BookingID  uuid.UUID
	AuthorID   uuid.UUID
	AuthorName string
	Body       string
	Pinned     bool
	CreatedAt  time.Time
}

// NewNote validates and builds a note.
func NewNote(bookingID, authorID uuid.UUID, body string, pinned bool, now time.Time) (*Note, error) {
	body = strings.TrimSpace(body)
	switch {
	case body == "":
		return nil, shared.NewValidation("note body is required")
	case utf8.RuneCountInString(body) > MaxNoteLen:
		return nil, shared.NewValidation("note body is too long")
	}
	return &Note{ID: uuid.New(), BookingID: bookingID, AuthorID: authorID, Body: body, Pinned: pinned, CreatedAt: now}, nil
}

// CanDelete restricts deletion to the author.
func (n *Note) CanDelete(actor uuid.UUID) bool {
	return n.AuthorID == actor
}

// Change request kinds.
const (
	ChangeDate  = "date_change"
	ChangeName  = "name_change"
	ChangeRoute = "route_change"
	ChangeOther = "other"
)

func ChangeKinds() []string { return []string{ChangeDate, ChangeName, ChangeRoute, ChangeOther} }

// Change request statuses.
const (
	ChangeRequested = "requested"
	ChangeCompleted = "completed"
	ChangeRejected  = "rejected"
)

const MaxChangeDetailsLen = 2000

// ChangeRequest is a reissue / exchange request. Completing one marks the
// booking reissued.
type ChangeRequest struct {
	ID              uuid.UUID
	BookingID       uuid.UUID
	Kind            string
	Details         string
	Status          string
	RequestedBy     uuid.UUID
	RequestedByName string
	ResolvedBy      *uuid.UUID
	ResolutionNote  string
	CreatedAt       time.Time
	ResolvedAt      *time.Time
}

// NewChangeRequest opens a request on a booking that can still change.
func NewChangeRequest(b *Booking, kind, details string, actor uuid.UUID, now time.Time) (*ChangeRequest, error) {
	if b.Status.Terminal() {
		return nil, shared.NewInvalidState("cannot request changes on a " + string(b.Status) + " booking")
	}
	kind = strings.TrimSpace(kind)
	details = strings.TrimSpace(details)
	switch {
	case !oneOf(kind, ChangeKinds()):
		return nil, shared.NewValidation("unknown change kind")
	case details == "":
		return nil, shared.NewValidation("details are required")
	case utf8.RuneCountInString(details) > MaxChangeDetailsLen:
		return nil, shared.NewValidation("details are too long")
	}
	return &ChangeRequest{
		ID: uuid.New(), BookingID: b.ID, Kind: kind, Details: details,
		Status: ChangeRequested, RequestedBy: actor, CreatedAt: now,
	}, nil
}

// Resolve closes an open request as completed or rejected; a rejection
// needs a note so the requester learns why.
func (c *ChangeRequest) Resolve(status, note string, actor uuid.UUID, now time.Time) error {
	if c.Status != ChangeRequested {
		return shared.NewInvalidState("change request is already " + c.Status)
	}
	note = strings.TrimSpace(note)
	switch status {
	case ChangeCompleted:
	case ChangeRejected:
		if note == "" {
			return shared.NewValidation("a note is required to reject a change")
		}
	default:
		return shared.NewValidation("status must be completed or rejected")
	}
	if utf8.RuneCountInString(note) > MaxChangeDetailsLen {
		return shared.NewValidation("note is too long")
	}
	c.Status = status
	c.ResolutionNote = note
	c.ResolvedBy = &actor
	c.ResolvedAt = &now
	return nil
}

// Activity is one entry of a booking's audit trail, reduced to the fields
// staff may see (no snapshots, no costs).
type Activity struct {
	ID         uuid.UUID
	Action     string
	EntityType string
	ActorName  string
	ActorType  string
	Details    map[string]any
	CreatedAt  time.Time
}

// ActivityDetailKeys whitelists audit payload keys shown in the activity feed.
var ActivityDetailKeys = []string{
	"status", "hold_expires_at", "status_reason", "amount", "currency", "method", "channel",
	"document", "kind", "pax_count", "full_name", "pnr", "service_type", "supplier_source",
	"discount_amt", "total_amount", "label", "event_type", "outcome", "note",
}

// CollabStore persists notes, change requests and the profile.
type CollabStore interface {
	UpdateProfile(ctx context.Context, b *Booking) error
	SaveHold(ctx context.Context, b *Booking) error
	IncrementReissue(ctx context.Context, bookingID uuid.UUID) error

	CreateNote(ctx context.Context, n *Note) error
	FindNote(ctx context.Context, bookingID, noteID uuid.UUID) (*Note, error)
	DeleteNote(ctx context.Context, bookingID, noteID uuid.UUID) error
	ListNotes(ctx context.Context, bookingID uuid.UUID) ([]Note, error)

	CreateChange(ctx context.Context, c *ChangeRequest) error
	FindChangeForUpdate(ctx context.Context, bookingID, changeID uuid.UUID) (*ChangeRequest, error)
	ResolveChange(ctx context.Context, c *ChangeRequest) error
	ListChanges(ctx context.Context, bookingID uuid.UUID) ([]ChangeRequest, error)

	Stats(ctx context.Context, f ListFilter) (Stats, error)
	ListActivity(ctx context.Context, bookingID uuid.UUID, limit int) ([]Activity, error)
}
