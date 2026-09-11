package document

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

type presignRequest struct {
	RelatedType string    `json:"related_type"`
	RelatedID   uuid.UUID `json:"related_id"`
	Kind        string    `json:"kind"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
}

type completeRequest struct {
	SizeBytes int64 `json:"size_bytes"`
}

func (h Handler) PresignUpload(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var req presignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.PresignUpload(r.Context(), appsvc.PresignUploadInput{
		BranchID:    claims.BranchID,
		RelatedType: req.RelatedType,
		RelatedID:   req.RelatedID,
		Kind:        req.Kind,
		FileName:    req.FileName,
		ContentType: req.ContentType,
		UploadedBy:  claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, res)
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	doc, err := h.Svc.Get(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, doc)
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	relatedType := r.URL.Query().Get("related_type")
	relatedID, err := uuid.Parse(r.URL.Query().Get("related_id"))
	if err != nil {
		response.Error(w, shared.NewValidation("related_id must be a uuid"))
		return
	}
	items, err := h.Svc.ListByRelated(r.Context(), relatedType, relatedID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) Complete(w http.ResponseWriter, r *http.Request) {
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
	var req completeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	doc, err := h.Svc.CompleteUpload(r.Context(), appsvc.CompleteUploadInput{
		DocumentID: id,
		SizeBytes:  req.SizeBytes,
		ActorID:    claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, doc)
}

func (h Handler) PresignDownload(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	res, err := h.Svc.PresignDownload(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, res)
}
