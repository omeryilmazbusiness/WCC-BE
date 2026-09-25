package ai

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ProviderRegistry resolves BYO LLM adapters (DIP).
type ProviderRegistry interface {
	Get(p domain.Provider) (domain.CompletionProvider, bool)
}

// LeadPriorityWriter persists deterministic scores (ISP).
type LeadPriorityWriter interface {
	UpdateLeadPriority(ctx context.Context, leadID uuid.UUID, score int, band domain.PriorityBand, signals json.RawMessage) error
}

// DashboardFacts supplies authorized manager context for summaries (authorized context only).
type DashboardFacts struct {
	LeadsOpen      int            `json:"leads_open"`
	TasksOverdue   int            `json:"tasks_overdue"`
	BookingsUnpaid int            `json:"bookings_unpaid"`
	MissingDocs    int            `json:"missing_docs"`
	Attention      []string       `json:"attention"`
	TargetStatus   string         `json:"target_status"`
	TargetLabel    string         `json:"target_label"`
	CollectedAmt   int64          `json:"collected_amt"`
	Extra          map[string]any `json:"extra,omitempty"`
}

type DashboardReader interface {
	Facts(ctx context.Context, branchID uuid.UUID) (*DashboardFacts, error)
}

type ConversationReader interface {
	Messages(ctx context.Context, conversationID, branchID uuid.UUID, limit int) (subject string, lines []string, err error)
}

type LeadReader interface {
	LoadSignals(ctx context.Context, leadID, branchID uuid.UUID) (domain.LeadSignals, string, error) // signals + display name
}

type TargetReader interface {
	LoadProgress(ctx context.Context, targetID, branchID uuid.UUID) (label string, target, actual, expected int64, status string, err error)
}

type SetupInput struct {
	BranchID uuid.UUID
	ActorID  uuid.UUID
	Provider domain.Provider
	APIKey   string
	Model    string
	Enabled  bool
}

type Service struct {
	repo     domain.Repository
	registry ProviderRegistry
	leadsPri LeadPriorityWriter
	dash     DashboardReader
	inbox    ConversationReader
	leads    LeadReader
	targets  TargetReader
}

func NewService(repo domain.Repository, registry ProviderRegistry) *Service {
	return &Service{repo: repo, registry: registry}
}

func (s *Service) SetLeadPriorityWriter(w LeadPriorityWriter) { s.leadsPri = w }
func (s *Service) SetDashboardReader(r DashboardReader)       { s.dash = r }
func (s *Service) SetConversationReader(r ConversationReader) { s.inbox = r }
func (s *Service) SetLeadReader(r LeadReader)                 { s.leads = r }
func (s *Service) SetTargetReader(r TargetReader)             { s.targets = r }

// GetSetup returns public AI settings (no api key).
func (s *Service) GetSetup(ctx context.Context, branchID uuid.UUID) (map[string]any, error) {
	st, err := s.repo.GetSettings(ctx, branchID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return map[string]any{
			"configured": false, "enabled": false, "provider": "", "model": "",
			"key_hint": "", "setup_completed": false,
			"accepted_providers": []string{"openai", "anthropic", "gemini"},
		}, nil
	}
	key := domain.ParseAPIKey(st.ConfigJSON)
	completed := st.SetupCompletedAt != nil
	return map[string]any{
		"configured":       key != "" && domain.ValidProvider(st.Provider),
		"enabled":          st.Enabled && key != "",
		"provider":         st.Provider,
		"model":            st.Model,
		"key_hint":         domain.MaskedKeyHint(key),
		"setup_completed":  completed,
		"accepted_providers": []string{"openai", "anthropic", "gemini"},
		"updated_at":       st.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// CompleteSetup stores BYO provider key (first-run wizard / admin).
func (s *Service) CompleteSetup(ctx context.Context, in SetupInput) (map[string]any, error) {
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id required")
	}
	if !domain.ValidProvider(in.Provider) {
		return nil, shared.NewValidation("provider must be openai, anthropic, or gemini")
	}
	key := strings.TrimSpace(in.APIKey)
	if key == "" {
		// allow update of model/enabled without rotating key
		existing, err := s.repo.GetSettings(ctx, in.BranchID)
		if err != nil {
			return nil, err
		}
		if existing == nil || domain.ParseAPIKey(existing.ConfigJSON) == "" {
			return nil, shared.NewValidation("api_key is required")
		}
		key = domain.ParseAPIKey(existing.ConfigJSON)
	}
	model := strings.TrimSpace(in.Model)
	if model == "" {
		model = domain.DefaultModel(in.Provider)
	}
	cfg, _ := json.Marshal(map[string]string{"api_key": key})
	now := time.Now().UTC()
	actor := in.ActorID
	st := &domain.Settings{
		BranchID: in.BranchID, Provider: in.Provider, Model: model,
		Enabled: in.Enabled, ConfigJSON: cfg,
		SetupCompletedAt: &now, UpdatedBy: &actor, UpdatedAt: now, CreatedAt: now,
	}
	if err := s.repo.UpsertSettings(ctx, st); err != nil {
		return nil, err
	}
	return s.GetSetup(ctx, in.BranchID)
}

func (s *Service) Disable(ctx context.Context, branchID, actorID uuid.UUID) (map[string]any, error) {
	st, err := s.repo.GetSettings(ctx, branchID)
	if err != nil || st == nil {
		return nil, shared.NewNotFound("ai_settings")
	}
	st.Enabled = false
	now := time.Now().UTC()
	st.UpdatedAt = now
	st.UpdatedBy = &actorID
	if err := s.repo.UpsertSettings(ctx, st); err != nil {
		return nil, err
	}
	return s.GetSetup(ctx, branchID)
}

func (s *Service) resolve(ctx context.Context, branchID uuid.UUID) (*domain.Settings, domain.CompletionProvider, string, error) {
	st, err := s.repo.GetSettings(ctx, branchID)
	if err != nil {
		return nil, nil, "", err
	}
	if st == nil || !st.Enabled {
		return nil, nil, "", domain.ErrNotConfigured()
	}
	key := domain.ParseAPIKey(st.ConfigJSON)
	if key == "" || !domain.ValidProvider(st.Provider) {
		return nil, nil, "", domain.ErrNotConfigured()
	}
	p, ok := s.registry.Get(st.Provider)
	if !ok {
		return nil, nil, "", shared.NewValidation("ai provider adapter missing")
	}
	return st, p, key, nil
}

func (s *Service) record(ctx context.Context, branchID uuid.UUID, actor *uuid.UUID, kind domain.Kind, st *domain.Settings, scope, output any, inputHash, status, errMsg string) (*domain.Run, error) {
	scopeRaw, _ := json.Marshal(scope)
	outRaw, _ := json.Marshal(output)
	if len(scopeRaw) == 0 {
		scopeRaw = []byte(`{}`)
	}
	if len(outRaw) == 0 {
		outRaw = []byte(`{}`)
	}
	prov := domain.ProviderNone
	model := ""
	if st != nil {
		prov = st.Provider
		model = st.Model
	}
	run := &domain.Run{
		ID: uuid.New(), BranchID: branchID, ActorID: actor, Kind: kind,
		Provider: prov, Model: model, ScopeJSON: scopeRaw, InputHash: inputHash,
		OutputJSON: outRaw, Status: status, ErrorMessage: errMsg, CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.InsertRun(ctx, run); err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Service) complete(ctx context.Context, st *domain.Settings, p domain.CompletionProvider, key string, req domain.CompletionRequest) (*domain.CompletionResponse, error) {
	if req.Model == "" {
		req.Model = st.Model
	}
	return p.Complete(ctx, key, req)
}

// DailySummary — T-190 / T-197.
func (s *Service) DailySummary(ctx context.Context, branchID, actorID uuid.UUID) (map[string]any, error) {
	facts := &DashboardFacts{}
	if s.dash != nil {
		f, err := s.dash.Facts(ctx, branchID)
		if err != nil {
			return nil, err
		}
		if f != nil {
			facts = f
		}
	}
	scope := map[string]any{"facts": facts}
	hash := domain.HashInput("daily", branchID.String(), time.Now().UTC().Format("2006-01-02"))

	st, p, key, err := s.resolve(ctx, branchID)
	if err != nil {
		// Deterministic fallback when AI not configured
		out := map[string]any{
			"headline": "Daily briefing (rules engine)",
			"bullets": []string{
				formatInt("Open leads", facts.LeadsOpen),
				formatInt("Overdue tasks", facts.TasksOverdue),
				formatInt("Unpaid bookings", facts.BookingsUnpaid),
				formatInt("Missing docs", facts.MissingDocs),
			},
			"attention": facts.Attention,
			"source":    "deterministic",
			"ai_enabled": false,
		}
		run, _ := s.record(ctx, branchID, &actorID, domain.KindDailySummary, nil, scope, out, hash, "skipped", "ai_not_configured")
		out["run_id"] = runID(run)
		return out, nil
	}

	userPrompt := "Summarize this branch operations snapshot for a Hajj/Umrah CRM manager in under 120 words. Use bullet points. Do not invent numbers.\n" + mustJSON(facts)
	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{
		System: "You are an operations analyst. Money/SLA figures are already computed — explain only. JSON: {\"headline\":\"\",\"bullets\":[],\"focus\":\"\"}",
		User: userPrompt, JSONMode: true, MaxTokens: 500,
	})
	if err != nil {
		out := map[string]any{
			"headline": "Daily briefing (provider unavailable — rules engine)",
			"bullets": []string{
				formatInt("Open leads", facts.LeadsOpen),
				formatInt("Overdue tasks", facts.TasksOverdue),
				formatInt("Unpaid bookings", facts.BookingsUnpaid),
				formatInt("Missing docs", facts.MissingDocs),
			},
			"attention":  facts.Attention,
			"source":     "deterministic",
			"ai_enabled": true,
			"provider_error": trimErr(err),
		}
		run, _ := s.record(ctx, branchID, &actorID, domain.KindDailySummary, st, scope, out, hash, "error", err.Error())
		out["run_id"] = runID(run)
		return out, nil
	}
	out := parseJSONObject(resp.Text)
	out["source"] = "ai"
	out["model"] = resp.Model
	out["ai_enabled"] = true
	run, _ := s.record(ctx, branchID, &actorID, domain.KindDailySummary, st, scope, out, hash, "ok", "")
	out["run_id"] = runID(run)
	return out, nil
}

// ConversationAssist — T-191 / T-192.
func (s *Service) ConversationAssist(ctx context.Context, branchID, actorID, conversationID uuid.UUID) (map[string]any, error) {
	if s.inbox == nil {
		return nil, shared.NewValidation("conversation reader not wired")
	}
	subject, lines, err := s.inbox.Messages(ctx, conversationID, branchID, 40)
	if err != nil {
		return nil, err
	}
	scope := map[string]any{"conversation_id": conversationID, "subject": subject, "message_count": len(lines)}
	hash := domain.HashInput("conv", conversationID.String(), strings.Join(lines, "\n"))

	st, p, key, err := s.resolve(ctx, branchID)
	if err != nil {
		out := map[string]any{
			"summary": "AI not configured — open setup to enable conversation assist.",
			"next_step": "Configure OpenAI, Claude, or Gemini API key.",
			"reply_draft": "",
			"auto_send": false,
			"source": "deterministic",
		}
		run, _ := s.record(ctx, branchID, &actorID, domain.KindConversationSum, nil, scope, out, hash, "skipped", "ai_not_configured")
		out["run_id"] = runID(run)
		return out, nil
	}

	transcript := strings.Join(lines, "\n")
	if len(transcript) > 8000 {
		transcript = transcript[len(transcript)-8000:]
	}
	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{
		System: "You help travel agency staff. Return JSON {\"summary\":\"\",\"next_step\":\"\",\"reply_draft\":\"\"}. Never claim payment amounts. Draft must be polite bilingual-ready EN. auto_send is always false.",
		User: "Subject: " + subject + "\n\nTranscript:\n" + transcript,
		JSONMode: true, MaxTokens: 700,
	})
	if err != nil {
		_, _ = s.record(ctx, branchID, &actorID, domain.KindConversationSum, st, scope, nil, hash, "error", err.Error())
		return nil, shared.NewValidation("ai_provider_error: " + trimErr(err))
	}
	out := parseJSONObject(resp.Text)
	out["auto_send"] = false // hard rule
	out["source"] = "ai"
	out["model"] = resp.Model
	run, _ := s.record(ctx, branchID, &actorID, domain.KindReplyDraft, st, scope, out, hash, "ok", "")
	out["run_id"] = runID(run)
	return out, nil
}

// ScoreLead — T-193 deterministic + optional AI explain.
func (s *Service) ScoreLead(ctx context.Context, branchID, actorID, leadID uuid.UUID, explain bool) (map[string]any, error) {
	if s.leads == nil {
		return nil, shared.NewValidation("lead reader not wired")
	}
	signalsIn, name, err := s.leads.LoadSignals(ctx, leadID, branchID)
	if err != nil {
		return nil, err
	}
	score, band, signals := domain.ScoreLead(signalsIn)
	if s.leadsPri != nil {
		_ = s.leadsPri.UpdateLeadPriority(ctx, leadID, score, band, domain.SignalsJSON(signals))
	}
	out := map[string]any{
		"lead_id": leadID, "name": name,
		"priority_score": score, "priority_band": band,
		"signals": signals, "source": "deterministic",
	}
	if !explain {
		return out, nil
	}
	st, p, key, err := s.resolve(ctx, branchID)
	if err != nil {
		out["explanation"] = "Priority is computed from CRM signals (not AI)."
		return out, nil
	}
	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{
		System: "Explain lead priority briefly for a sales manager. Do not change the score. JSON {\"explanation\":\"\"}",
		User: mustJSON(out), JSONMode: true, MaxTokens: 250,
	})
	if err == nil {
		parsed := parseJSONObject(resp.Text)
		out["explanation"] = parsed["explanation"]
		out["model"] = resp.Model
		hash := domain.HashInput("lead", leadID.String(), mustJSON(signals))
		run, _ := s.record(ctx, branchID, &actorID, domain.KindLeadExplain, st, map[string]any{"lead_id": leadID}, out, hash, "ok", "")
		out["run_id"] = runID(run)
	}
	return out, nil
}

// TargetInsight — T-194 (deterministic base + optional AI narrative).
func (s *Service) TargetInsight(ctx context.Context, branchID, actorID, targetID uuid.UUID) (map[string]any, error) {
	if s.targets == nil {
		return nil, shared.NewValidation("target reader not wired")
	}
	label, target, actual, expected, status, err := s.targets.LoadProgress(ctx, targetID, branchID)
	if err != nil {
		return nil, err
	}
	out := domain.DeterministicTargetInsight(label, target, actual, expected, status)
	st, p, key, err := s.resolve(ctx, branchID)
	if err != nil {
		return out, nil
	}
	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{
		System: "Add a short recovery narrative. Do not invent financial figures — use provided numbers only. JSON {\"narrative\":\"\",\"actions\":[]}",
		User: mustJSON(out), JSONMode: true, MaxTokens: 400,
	})
	if err != nil {
		return out, nil
	}
	parsed := parseJSONObject(resp.Text)
	out["narrative"] = parsed["narrative"]
	if acts, ok := parsed["actions"]; ok {
		out["ai_actions"] = acts
	}
	out["model"] = resp.Model
	out["enriched_by"] = "ai"
	out["source"] = "ai"
	hash := domain.HashInput("target", targetID.String(), mustJSON(out["variance"]))
	run, _ := s.record(ctx, branchID, &actorID, domain.KindTargetInsight, st, map[string]any{"target_id": targetID}, out, hash, "ok", "")
	out["run_id"] = runID(run)
	return out, nil
}

// OCRExtract — T-195; human must confirm before save.
func (s *Service) OCRExtract(ctx context.Context, branchID, actorID uuid.UUID, imageB64, mime, hint string) (map[string]any, error) {
	st, p, key, err := s.resolve(ctx, branchID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(imageB64) == "" {
		return nil, shared.NewValidation("image_base64 required")
	}
	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{
		System: "Extract travel document fields. Return JSON {\"fields\":{\"full_name\":\"\",\"passport_no\":\"\",\"nationality\":\"\",\"date_of_birth\":\"\",\"expiry_date\":\"\"},\"confidence\":0-1}. Never claim verification. Human must confirm.",
		User: hint, ImageB64: imageB64, ImageMIME: mime, JSONMode: true, MaxTokens: 500,
	})
	if err != nil {
		_, _ = s.record(ctx, branchID, &actorID, domain.KindOCRExtract, st, map[string]any{"hint": hint}, nil, domain.HashInput(imageB64[:min(32, len(imageB64))]), "error", err.Error())
		return nil, shared.NewValidation("ai_provider_error: " + trimErr(err))
	}
	out := parseJSONObject(resp.Text)
	out["requires_confirmation"] = true
	out["source"] = "ai"
	out["model"] = resp.Model
	hash := domain.HashInput("ocr", branchID.String(), resp.Text)
	run, _ := s.record(ctx, branchID, &actorID, domain.KindOCRExtract, st, map[string]any{"hint": hint}, out, hash, "ok", "")
	out["run_id"] = runID(run)
	return out, nil
}

func (s *Service) Feedback(ctx context.Context, runID uuid.UUID, feedback string) error {
	return s.repo.UpdateRunFeedback(ctx, runID, strings.TrimSpace(feedback))
}

func formatInt(label string, n int) string {
	return label + ": " + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func parseJSONObject(text string) map[string]any {
	text = strings.TrimSpace(text)
	out := map[string]any{}
	if text == "" {
		return out
	}
	// strip markdown fences if present
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	_ = json.Unmarshal([]byte(text), &out)
	if len(out) == 0 {
		out["text"] = text
	}
	return out
}

func runID(r *domain.Run) string {
	if r == nil {
		return ""
	}
	return r.ID.String()
}

func trimErr(err error) string {
	s := err.Error()
	if len(s) > 240 {
		return s[:240]
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
