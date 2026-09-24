package document

import (
	"encoding/json"
	"net/http"
	"time"

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
	RelatedType   string     `json:"related_type"`
	RelatedID     uuid.UUID  `json:"related_id"`
	Kind          string     `json:"kind"`
	FileName      string     `json:"file_name"`
	ContentType   string     `json:"content_type"`
	ParticipantID *uuid.UUID `json:"participant_id"`
	ExpiresAt     *string    `json:"expires_at"`
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
	var expires *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse("2006-01-02", *req.ExpiresAt)
		if err != nil {
			response.Error(w, shared.NewValidation("expires_at must be YYYY-MM-DD"))
			return
		}
		expires = &t
	}
	res, err := h.Svc.PresignUpload(r.Context(), appsvc.PresignUploadInput{
		BranchID: claims.BranchID, RelatedType: req.RelatedType, RelatedID: req.RelatedID,
		Kind: req.Kind, FileName: req.FileName, ContentType: req.ContentType,
		UploadedBy: claims.UserID, ParticipantID: req.ParticipantID, ExpiresAt: expires,
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
		DocumentID: id, SizeBytes: req.SizeBytes, ActorID: claims.UserID,
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

func (h Handler) Classify(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	doc, err := h.Svc.Classify(r.Context(), id, body.Kind)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, doc)
}

func (h Handler) Submit(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	doc, err := h.Svc.Submit(r.Context(), id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, doc)
}

func (h Handler) Approve(w http.ResponseWriter, r *http.Request) {
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
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	doc, err := h.Svc.Approve(r.Context(), id, claims.UserID, body.Note)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, doc)
}

func (h Handler) Reject(w http.ResponseWriter, r *http.Request) {
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
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	doc, err := h.Svc.Reject(r.Context(), id, claims.UserID, body.Note)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, doc)
}

func (h Handler) Replace(w http.ResponseWriter, r *http.Request) {
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
		FileName    string `json:"file_name"`
		ContentType string `json:"content_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	res, err := h.Svc.Replace(r.Context(), appsvc.ReplaceInput{
		DocumentID: id, FileName: body.FileName, ContentType: body.ContentType, UploadedBy: claims.UserID,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusCreated, res)
}

func (h Handler) ListPolicies(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	items, err := h.Svc.ListPolicies(r.Context(), claims.BranchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, items)
}

func (h Handler) UpsertPolicy(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	var body appsvc.UpsertPolicyInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	body.BranchID = claims.BranchID
	p, err := h.Svc.UpsertPolicy(r.Context(), body)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, p)
}

func (h Handler) Checklist(w http.ResponseWriter, r *http.Request) {
	bookingID, err := uuid.Parse(r.URL.Query().Get("booking_id"))
	if err != nil {
		response.Error(w, shared.NewValidation("booking_id must be a uuid"))
		return
	}
	cl, err := h.Svc.Checklist(r.Context(), bookingID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, cl)
}

func (h Handler) MissingDocs(w http.ResponseWriter, r *http.Request) {
	departureID, err := uuid.Parse(r.URL.Query().Get("departure_id"))
	if err != nil {
		response.Error(w, shared.NewValidation("departure_id must be a uuid"))
		return
	}
	rows, err := h.Svc.DepartureMissingDocs(r.Context(), departureID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, rows)
}

func (h Handler) ProcessExpiryReminders(w http.ResponseWriter, r *http.Request) {
	if _, ok := middleware.ClaimsFrom(r.Context()); !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	n, err := h.Svc.ProcessExpiryReminders(r.Context(), time.Now().UTC(), 100)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"expired": n})
}
