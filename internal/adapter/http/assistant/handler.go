// Package assistant serves the in-app AI assistant: its protocol and chat.
package assistant

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/middleware"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appsvc "github.com/wodi-crm/wodi-crm-be/internal/app/assistant"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/assistant"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// maxChatBody bounds one chat request: 50 turns of history plus the question.
const maxChatBody = 256 << 10

type Handler struct {
	Svc *appsvc.Service
}

func (h Handler) viewer(r *http.Request) (appsvc.Viewer, error) {
	claims, ok := middleware.ClaimsFrom(r.Context())
	if !ok {
		return appsvc.Viewer{}, shared.NewUnauthorized("unauthenticated")
	}
	branchID, err := request.TargetBranch(r)
	if err != nil {
		return appsvc.Viewer{}, err
	}
	role := claims.Role
	return appsvc.Viewer{
		UserID: claims.UserID, BranchID: branchID,
		Can: func(p string) bool { return platformauth.HasPermission(role, platformauth.Permission(p)) },
	}, nil
}

type quotaView struct {
	Limit     int    `json:"limit"`
	Used      int    `json:"used"`
	Remaining int    `json:"remaining"`
	Mode      string `json:"mode"`
	ResetsAt  string `json:"resets_at"`
}

func toQuota(q domain.Quota) quotaView {
	return quotaView{Limit: q.Limit, Used: q.Used, Remaining: q.Remaining(), Mode: string(q.Mode()), ResetsAt: q.ResetsAt.UTC().Format(time.RFC3339)}
}

// Protocol lists the assistant's rules, capabilities (with `allowed` for the
// caller), limits and today's quota.
func (h Handler) Protocol(w http.ResponseWriter, r *http.Request) {
	v, err := h.viewer(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	p, err := h.Svc.Protocol(r.Context(), v)
	if err != nil {
		response.Error(w, err)
		return
	}
	caps := make([]map[string]any, 0, len(p.Capabilities))
	for _, c := range p.Capabilities {
		caps = append(caps, map[string]any{
			"id": c.ID, "kind": c.Kind, "requires": c.Requires, "allowed": c.Allowed,
			"uses_model": c.UsesModel(), "uses_data": c.NeedsFacts, "max_output_tokens": c.MaxOutputTokens,
		})
	}
	response.JSON(w, http.StatusOK, map[string]any{
		"version": p.Version, "rules": p.Rules, "capabilities": caps,
		"ai_configured": p.AIConfigured, "faq_first": true, "quota": toQuota(p.Quota),
		"limits": map[string]any{
			"max_prompt_chars": domain.MaxPromptChars, "history_turns": domain.HistoryTurns,
			"history_chars": domain.HistoryChars, "cache_minutes": int(domain.CacheTTL / time.Minute),
		},
	})
}

type chatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Context struct {
		Locale string `json:"locale"`
		Screen string `json:"screen"`
	} `json:"context"`
}

// Chat answers the last user message through the cheapest-first pipeline.
func (h Handler) Chat(w http.ResponseWriter, r *http.Request) {
	v, err := h.viewer(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	var body chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxChatBody)).Decode(&body); err != nil {
		response.Error(w, shared.NewValidation("invalid json"))
		return
	}
	turns := make([]domain.Turn, 0, len(body.Messages))
	for _, m := range body.Messages {
		turns = append(turns, domain.Turn{Role: m.Role, Content: m.Content})
	}
	a, err := h.Svc.Ask(r.Context(), v, appsvc.Request{Messages: turns, Locale: body.Context.Locale, Screen: body.Context.Screen})
	if err != nil {
		response.Error(w, err)
		return
	}
	out := map[string]any{
		"reply": a.Reply, "capability": a.Capability, "source": a.Source, "quota": toQuota(a.Quota),
	}
	if a.Notice != domain.NoticeNone {
		out["notice"] = a.Notice
	}
	response.JSON(w, http.StatusOK, out)
}
