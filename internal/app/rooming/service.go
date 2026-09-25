package rooming

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/rooming"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Service struct {
	repo domain.Repository
	now  func() time.Time
}

func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type CreateRoomInput struct {
	BranchID  uuid.UUID       `json:"-"`
	Label     string          `json:"label"`
	RoomType  domain.RoomType `json:"room_type"`
	Capacity  int             `json:"capacity"`
	Notes     string          `json:"notes"`
	SortOrder int             `json:"sort_order"`
}

type UpdateRoomInput struct {
	Label     *string          `json:"label"`
	RoomType  *domain.RoomType `json:"room_type"`
	Capacity  *int             `json:"capacity"`
	Notes     *string          `json:"notes"`
	SortOrder *int             `json:"sort_order"`
}

type AssignInput struct {
	ParticipantID uuid.UUID `json:"participant_id"`
	BookingID     uuid.UUID `json:"booking_id"`
	ActorID       uuid.UUID `json:"-"`
}

func (s *Service) ListRooms(ctx context.Context, departureID uuid.UUID) ([]domain.Room, error) {
	return s.repo.ListRooms(ctx, departureID)
}

func (s *Service) CreateRoom(ctx context.Context, departureID uuid.UUID, in CreateRoomInput) (*domain.Room, error) {
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	room := &domain.Room{
		ID: uuid.New(), DepartureID: departureID, BranchID: in.BranchID,
		Label: in.Label, RoomType: in.RoomType, Capacity: in.Capacity,
		Notes: in.Notes, SortOrder: in.SortOrder, CreatedAt: s.now(),
	}
	if err := room.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.CreateRoom(ctx, room); err != nil {
		return nil, err
	}
	return room, nil
}

func (s *Service) UpdateRoom(ctx context.Context, departureID, roomID uuid.UUID, in UpdateRoomInput) (*domain.Room, error) {
	rooms, err := s.repo.ListRooms(ctx, departureID)
	if err != nil {
		return nil, err
	}
	var found *domain.Room
	for i := range rooms {
		if rooms[i].ID == roomID {
			found = &rooms[i]
			break
		}
	}
	if found == nil {
		return nil, shared.NewNotFound("room")
	}
	if in.Label != nil {
		found.Label = *in.Label
	}
	if in.RoomType != nil {
		found.RoomType = *in.RoomType
	}
	if in.Capacity != nil {
		found.Capacity = *in.Capacity
	}
	if in.Notes != nil {
		found.Notes = *in.Notes
	}
	if in.SortOrder != nil {
		found.SortOrder = *in.SortOrder
	}
	if err := found.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateRoom(ctx, found); err != nil {
		return nil, err
	}
	return found, nil
}

func (s *Service) DeleteRoom(ctx context.Context, departureID, roomID uuid.UUID) error {
	rooms, err := s.repo.ListRooms(ctx, departureID)
	if err != nil {
		return err
	}
	ok := false
	for _, r := range rooms {
		if r.ID == roomID {
			ok = true
			break
		}
	}
	if !ok {
		return shared.NewNotFound("room")
	}
	if err := s.repo.DeleteRoom(ctx, roomID); err != nil {
		return shared.NewNotFound("room")
	}
	return nil
}

func (s *Service) Assign(ctx context.Context, departureID, roomID uuid.UUID, in AssignInput) (*domain.Assignment, error) {
	if in.ParticipantID == uuid.Nil || in.BookingID == uuid.Nil {
		return nil, shared.NewValidation("participant_id and booking_id are required")
	}
	rooms, err := s.repo.ListRooms(ctx, departureID)
	if err != nil {
		return nil, err
	}
	var room *domain.Room
	for i := range rooms {
		if rooms[i].ID == roomID {
			room = &rooms[i]
			break
		}
	}
	if room == nil {
		return nil, shared.NewNotFound("room")
	}
	if room.IsFull() {
		return nil, shared.NewInvalidState("room is at capacity")
	}
	now := s.now()
	actor := in.ActorID
	a := &domain.Assignment{
		ID: uuid.New(), RoomID: roomID, ParticipantID: in.ParticipantID,
		BookingID: in.BookingID, AssignedAt: now,
	}
	if actor != uuid.Nil {
		a.AssignedBy = &actor
	}
	if err := s.repo.Assign(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Service) Unassign(ctx context.Context, departureID, participantID uuid.UUID) error {
	if participantID == uuid.Nil {
		return shared.NewValidation("participant_id is required")
	}
	_ = departureID // scoped by route; assignment uniqueness is global per participant
	if err := s.repo.Unassign(ctx, participantID); err != nil {
		return shared.NewNotFound("assignment")
	}
	return nil
}

func (s *Service) GroupList(ctx context.Context, departureID uuid.UUID) ([]domain.GroupListRow, error) {
	return s.repo.GroupList(ctx, departureID)
}

// ExportCSV builds a group-list CSV (header + rows).
func ExportCSV(rows []domain.GroupListRow) string {
	var b strings.Builder
	b.WriteString("participant_id,booking_id,full_name,passport_no,nationality,room_label,unassigned\n")
	for _, r := range rows {
		room := r.RoomLabel
		unassigned := "0"
		if r.Unassigned {
			unassigned = "1"
		}
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,%s,%s\n",
			r.ParticipantID, r.BookingID,
			csvEscape(r.FullName), csvEscape(r.PassportNo), csvEscape(r.Nationality),
			csvEscape(room), unassigned,
		)
	}
	return b.String()
}

func csvEscape(s string) string {
	if strings.ContainsAny(s, ",\"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}
