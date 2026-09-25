package rooming

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/rooming"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/rooming"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func mapRoom(r *domain.Room) map[string]any {
	return map[string]any{
		"id": r.ID, "departure_id": r.DepartureID, "branch_id": r.BranchID,
		"label": r.Label, "room_type": r.RoomType, "capacity": r.Capacity,
		"notes": r.Notes, "sort_order": r.SortOrder, "assigned": r.Assigned,
		"is_full":    r.IsFull(),
		"created_at": r.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (h Handler) ListRooms(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	items, err := h.Svc.ListRooms(r.Context(), depID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapRoom(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CreateRoom(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	var body appsvc.CreateRoomInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	body.BranchID = claims.BranchID
	room, err := h.Svc.CreateRoom(r.Context(), depID, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapRoom(room))
}

func (h Handler) PatchRoom(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	roomID, err := uuid.Parse(chi.URLParam(r, "roomId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid room id"))
		return
	}
	var body appsvc.UpdateRoomInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	room, err := h.Svc.UpdateRoom(r.Context(), depID, roomID, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapRoom(room))
}

func (h Handler) DeleteRoom(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	roomID, err := uuid.Parse(chi.URLParam(r, "roomId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid room id"))
		return
	}
	if err := h.Svc.DeleteRoom(r.Context(), depID, roomID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) Assign(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	roomID, err := uuid.Parse(chi.URLParam(r, "roomId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid room id"))
		return
	}
	var body appsvc.AssignInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	body.ActorID = claims.UserID
	a, err := h.Svc.Assign(r.Context(), depID, roomID, body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"id": a.ID, "room_id": a.RoomID, "participant_id": a.ParticipantID,
		"booking_id": a.BookingID, "assigned_at": a.AssignedAt.UTC().Format(time.RFC3339Nano),
		"assigned_by": a.AssignedBy,
	})
}

func (h Handler) Unassign(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	pid, err := uuid.Parse(chi.URLParam(r, "participantId"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid participant id"))
		return
	}
	if err := h.Svc.Unassign(r.Context(), depID, pid); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) GroupList(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	items, err := h.Svc.GroupList(r.Context(), depID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) GroupListCSV(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	depID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid departure id"))
		return
	}
	items, err := h.Svc.GroupList(r.Context(), depID)
	if err != nil {
		response.Error(w, err)
		return
	}
	csv := appsvc.ExportCSV(items)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="group-list.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(csv))
}
