package rooming

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type RoomType string

const (
	RoomSingle RoomType = "single"
	RoomDouble RoomType = "double"
	RoomTriple RoomType = "triple"
	RoomQuad   RoomType = "quad"
	RoomSuite  RoomType = "suite"
	RoomOther  RoomType = "other"
)

func ValidRoomType(t RoomType) bool {
	switch t {
	case RoomSingle, RoomDouble, RoomTriple, RoomQuad, RoomSuite, RoomOther:
		return true
	default:
		return false
	}
}

type Room struct {
	ID          uuid.UUID
	DepartureID uuid.UUID
	BranchID    uuid.UUID
	Label       string
	RoomType    RoomType
	Capacity    int
	Notes       string
	SortOrder   int
	CreatedAt   time.Time
	Assigned    int // computed
}

func (r *Room) Normalize() error {
	r.Label = strings.TrimSpace(r.Label)
	r.Notes = strings.TrimSpace(r.Notes)
	if !ValidRoomType(r.RoomType) {
		r.RoomType = RoomDouble
	}
	if r.Capacity <= 0 {
		return shared.NewValidation("capacity must be > 0")
	}
	if r.Label == "" {
		r.Label = string(r.RoomType)
	}
	return nil
}

func (r *Room) IsFull() bool {
	return r.Assigned >= r.Capacity
}

type Assignment struct {
	ID            uuid.UUID
	RoomID        uuid.UUID
	ParticipantID uuid.UUID
	BookingID     uuid.UUID
	AssignedAt    time.Time
	AssignedBy    *uuid.UUID
	// Denormalized for group list
	ParticipantName string
	PassportNo      string
}

type GroupListRow struct {
	ParticipantID   uuid.UUID  `json:"participant_id"`
	BookingID       uuid.UUID  `json:"booking_id"`
	FullName        string     `json:"full_name"`
	PassportNo      string     `json:"passport_no"`
	Nationality     string     `json:"nationality"`
	RoomID          *uuid.UUID `json:"room_id,omitempty"`
	RoomLabel       string     `json:"room_label"`
	Unassigned      bool       `json:"unassigned"`
}

type Repository interface {
	ListRooms(ctx context.Context, departureID uuid.UUID) ([]Room, error)
	CreateRoom(ctx context.Context, r *Room) error
	UpdateRoom(ctx context.Context, r *Room) error
	DeleteRoom(ctx context.Context, id uuid.UUID) error
	Assign(ctx context.Context, a *Assignment) error
	Unassign(ctx context.Context, participantID uuid.UUID) error
	ListAssignments(ctx context.Context, departureID uuid.UUID) ([]Assignment, error)
	GroupList(ctx context.Context, departureID uuid.UUID) ([]GroupListRow, error)
}
