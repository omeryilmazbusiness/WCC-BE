package document

import (
	"encoding/json"
	"net/http"

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
