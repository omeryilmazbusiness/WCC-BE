package importexport

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func mapJob(j *domain.ImportJob) map[string]any {
	if j == nil {
		return nil
	}
	return map[string]any{
		"id": j.ID, "branch_id": j.BranchID, "entity_type": j.EntityType, "mode": j.Mode,
		"status": j.Status, "file_name": j.FileName, "content_type": j.ContentType,
		"storage_key": j.StorageKey, "headers": j.Headers, "mapping": j.Mapping,
		"preview_rows": j.PreviewRows, "total_rows": j.TotalRows,
		"success_count": j.SuccessCount, "failed_count": j.FailedCount, "skipped_count": j.SkippedCount,
		"rollback_token": j.RollbackToken, "error_message": j.ErrorMessage,
		"created_by": j.CreatedBy,
		"created_at": j.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": j.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func mapTemplate(t domain.MappingTemplate) map[string]any {
	return map[string]any{
		"id": t.ID, "branch_id": t.BranchID, "name": t.Name,
		"entity_type": t.EntityType, "mapping": t.Mapping, "created_by": t.CreatedBy,
		"created_at": t.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (h Handler) Upload(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		response.Error(w, shared.NewValidation("multipart form required"))
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		response.Error(w, shared.NewValidation("file is required"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		response.Error(w, shared.NewValidation("cannot read file"))
		return
	}
	entity := domain.EntityType(r.FormValue("entity_type"))
	mode := domain.ImportMode(r.FormValue("mode"))
	ct := hdr.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	job, err := h.Svc.Upload(r.Context(), appsvc.UploadInput{
		BranchID: claims.BranchID, ActorID: claims.UserID,
		EntityType: entity, Mode: mode,
		FileName: hdr.Filename, ContentType: ct, FileBytes: data,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapJob(job))
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.List(r.Context(), claims.BranchID, 50)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapJob(&items[i]))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	job, err := h.Svc.Get(r.Context(), id, claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapJob(job))
}

func (h Handler) SetMapping(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body struct {
		Mapping map[string]string `json:"mapping"`
		Mode    string            `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	job, err := h.Svc.SetMapping(r.Context(), appsvc.MappingInput{
		JobID: id, BranchID: claims.BranchID, Mapping: body.Mapping, Mode: domain.ImportMode(body.Mode),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapJob(job))
}

func (h Handler) Validate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	job, err := h.Svc.Validate(r.Context(), id, claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapJob(job))
}

func (h Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	job, err := h.Svc.Confirm(r.Context(), id, claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapJob(job))
}

func (h Handler) Errors(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	csv, err := h.Svc.ErrorsCSV(r.Context(), id, claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=import-errors-"+id.String()+".csv")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csv)
}

func (h Handler) ListTemplates(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	entity := domain.EntityType(r.URL.Query().Get("entity_type"))
	items, err := h.Svc.ListTemplates(r.Context(), claims.BranchID, entity)
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, t := range items {
		out = append(out, mapTemplate(t))
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		Name       string            `json:"name"`
		EntityType string            `json:"entity_type"`
		Mapping    map[string]string `json:"mapping"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	t, err := h.Svc.CreateTemplate(r.Context(), appsvc.TemplateInput{
		BranchID: claims.BranchID, ActorID: claims.UserID,
		Name: body.Name, EntityType: domain.EntityType(body.EntityType), Mapping: body.Mapping,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, mapTemplate(*t))
}

func (h Handler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	if err := h.Svc.DeleteTemplate(r.Context(), id, claims.BranchID); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (h Handler) Export(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body struct {
		EntityType string `json:"entity_type"`
		Format     string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	if body.Format != "" && body.Format != "csv" {
		response.Error(w, shared.NewValidation("format must be csv"))
		return
	}
	csv, filename, err := h.Svc.ExportCSV(r.Context(), appsvc.ExportInput{
		BranchID: claims.BranchID, EntityType: domain.EntityType(body.EntityType),
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+filename)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(csv)
}

func (h Handler) Schemas(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	response.JSON(w, http.StatusOK, h.Svc.Schemas())
}
