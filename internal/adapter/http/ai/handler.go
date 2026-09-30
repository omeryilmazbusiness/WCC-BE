package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

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

// DailySummary returns the branch's latest AI briefing without calling the
// model; available is false until one has been generated.
func (h Handler) DailySummary(w http.ResponseWriter, r *http.Request) {
	h.latest(w, r, h.Svc.LatestDailySummary)
}

// GenerateDailySummary asks the model for a fresh briefing now.
func (h Handler) GenerateDailySummary(w http.ResponseWriter, r *http.Request) {
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
	out["available"] = true
	response.JSON(w, http.StatusOK, out)
}

// LostLeadsAnalysis returns the branch's latest lost-lead analysis.
func (h Handler) LostLeadsAnalysis(w http.ResponseWriter, r *http.Request) {
	h.latest(w, r, h.Svc.LatestLostLeadsAnalysis)
}

func (h Handler) latest(w http.ResponseWriter, r *http.Request, read func(context.Context, uuid.UUID) (map[string]any, error)) {
	branchID, err := request.TargetBranch(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	configured, err := h.Svc.Configured(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	out, err := read(r.Context(), branchID)
	if err != nil {
		response.Error(w, err)
		return
	}
	if out == nil {
		out = map[string]any{}
	}
	out["available"] = len(out) > 0
	out["ai_enabled"] = configured
	response.JSON(w, http.StatusOK, out)
}

// AnalyzeLostLeads runs the analysis now over the last seven days.
func (h Handler) AnalyzeLostLeads(w http.ResponseWriter, r *http.Request) {
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
	now := time.Now()
	out, err := h.Svc.LostLeadsAnalysis(r.Context(), branchID, claims.UserID, now.AddDate(0, 0, -7), now)
	if err != nil {
		response.Error(w, err)
		return
	}
	out["available"] = true
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

// LeadDraft reads a conversation with AI and returns a lead form prefill.
func (h Handler) LeadDraft(w http.ResponseWriter, r *http.Request) {
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
	out, err := h.Svc.LeadDraftFromConversation(r.Context(), branchID, claims.UserID, id)
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

// maxScoreBatch caps one ScoreLeads request: a board page of cards.
const maxScoreBatch = 100

// ScoreLeads scores many leads in one call so a board page needs one request,
// not one per card. Leads the caller cannot see are left out.
func (h Handler) ScoreLeads(w http.ResponseWriter, r *http.Request) {
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
	var req struct {
		LeadIDs []string `json:"lead_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	if len(req.LeadIDs) > maxScoreBatch {
		response.Error(w, shared.NewValidation("too many lead_ids"))
		return
	}
	out := make([]map[string]any, 0, len(req.LeadIDs))
	for _, raw := range req.LeadIDs {
		id, err := uuid.Parse(raw)
		if err != nil {
			response.Error(w, shared.NewValidation("invalid lead_ids"))
			return
		}
		score, err := h.Svc.ScoreLead(r.Context(), branchID, claims.UserID, id, false)
		if err != nil {
			continue
		}
		out = append(out, score)
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
