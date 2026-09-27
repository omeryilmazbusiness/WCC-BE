package report

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/report"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid "+name))
		return uuid.Nil, false
	}
	return id, true
}

func decodeSchedule(w http.ResponseWriter, r *http.Request) (appsvc.ScheduleInput, bool) {
	var in appsvc.ScheduleInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		response.Error(w, shared.NewValidation("invalid body"))
		return in, false
	}
	return in, true
}

// ListSchedules: GET /v1/reports/schedules.
func (h Handler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	out, err := h.Schedules.List(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

// CreateSchedule: POST /v1/reports/schedules.
func (h Handler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	in, ok := decodeSchedule(w, r)
	if !ok {
		return
	}
	out, err := h.Schedules.Create(r.Context(), branchID, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, out)
}

// UpdateSchedule: PATCH /v1/reports/schedules/{id}.
func (h Handler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	in, ok := decodeSchedule(w, r)
	if !ok {
		return
	}
	out, err := h.Schedules.Update(r.Context(), id, in)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

// DeleteSchedule: DELETE /v1/reports/schedules/{id}.
func (h Handler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	if err := h.Schedules.Delete(r.Context(), id); err != nil {
		response.Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListRuns: GET /v1/reports/runs?schedule_id=&limit=.
func (h Handler) ListRuns(w http.ResponseWriter, r *http.Request) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	scheduleID, err := request.OptionalUUID(r, "schedule_id")
	if err != nil {
		response.Error(w, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.Schedules.Runs(r.Context(), branchID, scheduleID, limit)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

// DownloadRun: GET /v1/reports/runs/{id}/download.
func (h Handler) DownloadRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "id")
	if !ok {
		return
	}
	run, err := h.Schedules.Download(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+run.Filename+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(run.Content)
}
