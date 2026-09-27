package ai

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) GetSetup(w http.ResponseWriter, r *http.Request) {
	_, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	out, err := h.Svc.GetSetup(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) CompleteSetup(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body struct {
		Provider string `json:"provider"`
		APIKey   string `json:"api_key"`
		Model    string `json:"model"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	out, err := h.Svc.CompleteSetup(r.Context(), appsvc.SetupInput{
		BranchID: branchID, ActorID: claims.UserID,
		Provider: domain.Provider(body.Provider), APIKey: body.APIKey,
		Model: body.Model, Enabled: enabled,
	})
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Disable(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	out, err := h.Svc.Disable(r.Context(), branchID, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) DailySummary(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	out, err := h.Svc.DailySummary(r.Context(), branchID, claims.UserID)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) ConversationAssist(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	out, err := h.Svc.ConversationAssist(r.Context(), branchID, claims.UserID, id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

// LeadPriority is the read path for list badges: always the deterministic score,
// never an LLM call, whatever the query says.
func (h Handler) LeadPriority(w http.ResponseWriter, r *http.Request) {
	h.scoreLead(w, r, false)
}

func (h Handler) ScoreLead(w http.ResponseWriter, r *http.Request) {
	explain := r.URL.Query().Get("explain") == "1" || r.URL.Query().Get("explain") == "true"
	h.scoreLead(w, r, explain)
}

func (h Handler) scoreLead(w http.ResponseWriter, r *http.Request, explain bool) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	out, err := h.Svc.ScoreLead(r.Context(), branchID, claims.UserID, id, explain)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) TargetInsight(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	out, err := h.Svc.TargetInsight(r.Context(), branchID, claims.UserID, id)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) OCRExtract(w http.ResponseWriter, r *http.Request) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		response.Error(w, shared.NewUnauthorized("unauthenticated"))
		return
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body struct {
		ImageBase64 string `json:"image_base64"`
		MIME        string `json:"mime"`
		Hint        string `json:"hint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	out, err := h.Svc.OCRExtract(r.Context(), branchID, claims.UserID, body.ImageBase64, body.MIME, body.Hint)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, out)
}

func (h Handler) Feedback(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, shared.NewValidation("invalid id"))
		return
	}
	var body struct {
		Feedback string `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	if err := h.Svc.Feedback(r.Context(), id, body.Feedback); err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
