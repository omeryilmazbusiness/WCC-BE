package visa

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Status string

const (
	StatusDraft      Status = "draft"
	StatusSubmitted  Status = "submitted"
	StatusProcessing Status = "processing"
	StatusApproved   Status = "approved"
	StatusRejected   Status = "rejected"
	StatusIssued     Status = "issued"
	StatusCancelled  Status = "cancelled"
)

var transitions = map[Status][]Status{
	StatusDraft:      {StatusSubmitted, StatusCancelled},
	StatusSubmitted:  {StatusProcessing, StatusRejected, StatusCancelled},
	StatusProcessing: {StatusApproved, StatusRejected, StatusIssued, StatusCancelled},
	StatusApproved:   {StatusIssued, StatusCancelled},
	StatusRejected:   {StatusDraft, StatusCancelled},
	StatusIssued:     {StatusCancelled},
	StatusCancelled:  {},
}

func ValidTransition(from, to Status) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func ValidStatus(s Status) bool {
	switch s {
	case StatusDraft, StatusSubmitted, StatusProcessing, StatusApproved, StatusRejected, StatusIssued, StatusCancelled:
		return true
	default:
		return false
	}
}

type VisaCase struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	BookingID     uuid.UUID
	ParticipantID *uuid.UUID
	CustomerID    *uuid.UUID
	Status        Status
	ExternalRef   string
	Notes         string
	SubmittedAt   *time.Time
	DecidedAt     *time.Time
	ExpiresAt     *time.Time
	CreatedBy     uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Event struct {
	ID         uuid.UUID
	VisaCaseID uuid.UUID
	FromStatus Status
	ToStatus   Status
	ActorID    *uuid.UUID
	Note       string
	CreatedAt  time.Time
}

func (v *VisaCase) TransitionTo(to Status, actorID uuid.UUID, note string) (*Event, error) {
	if !ValidStatus(to) {
		return nil, shared.NewValidation("invalid visa status")
	}
	if !ValidTransition(v.Status, to) {
		return nil, shared.NewInvalidState("cannot transition visa from " + string(v.Status) + " to " + string(to))
	}
	from := v.Status
	now := time.Now().UTC()
	v.Status = to
	v.UpdatedAt = now
	v.Notes = mergeNote(v.Notes, note)
	switch to {
	case StatusSubmitted:
		v.SubmittedAt = &now
	case StatusApproved, StatusRejected, StatusIssued:
		v.DecidedAt = &now
	}
	aid := actorID
	ev := &Event{
		ID:         uuid.New(),
		VisaCaseID: v.ID,
		FromStatus: from,
		ToStatus:   to,
		ActorID:    &aid,
		Note:       strings.TrimSpace(note),
		CreatedAt:  now,
	}
	return ev, nil
}

func mergeNote(existing, add string) string {
	add = strings.TrimSpace(add)
	if add == "" {
		return existing
	}
	if strings.TrimSpace(existing) == "" {
		return add
	}
	return existing + "\n" + add
}

type Repository interface {
	Create(ctx context.Context, v *VisaCase) error
	Update(ctx context.Context, v *VisaCase) error
	FindByID(ctx context.Context, id uuid.UUID) (*VisaCase, error)
	ListByBooking(ctx context.Context, bookingID uuid.UUID) ([]VisaCase, error)
	AppendEvent(ctx context.Context, e *Event) error
	ListEvents(ctx context.Context, visaCaseID uuid.UUID) ([]Event, error)
}
