package ai

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// LostLead is a lost lead's reason and context; it never carries the
// customer's name or contact details, so nothing personal reaches the model.
type LostLead struct {
	ReasonCode string `json:"reason"`
	Note       string `json:"note,omitempty"`
	Source     string `json:"source,omitempty"`
	FromStage  string `json:"lost_from,omitempty"`
}

type LostLeadReader interface {
	LostBetween(ctx context.Context, branchID uuid.UUID, from, to time.Time, limit int) ([]LostLead, error)
}

func (s *Service) SetLostLeadReader(r LostLeadReader) { s.lost = r }

type ReasonCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

const (
	lostLeadsLimit    = 300
	lostPromptLeads   = 150
	lostNoteMaxRunes  = 280
	lostMaxActions    = 5
	lostTextMaxRunes  = 220
	lostLeadsMaxToken = 900
)

const lostLeadsSystem = `You are a sales coach for a Hajj and Umrah travel agency. You read why sales leads were lost during one period and tell the sales team what to change.
Reply with JSON only: {"summary_en":"","summary_ar":"","actions_en":[],"actions_ar":[]}
Rules:
- summary: 1-2 short sentences naming the main reasons leads were lost. Use only the counts given.
- actions: 3 to 5 items, most impactful first. Each is one short imperative sentence (at most 14 words) the team can apply this week. Concrete, simple words, no jargon, no numbering.
- Base everything on the data. Do not invent facts, prices or names.
- The _ar fields carry the same content in clear Modern Standard Arabic.`

// LostLeadsAnalysis reads why the branch lost leads in [from, to), and asks
// the branch's AI for a short summary plus simple actions (English and
// Arabic). It needs AI. actorID is uuid.Nil for the scheduled run.
func (s *Service) LostLeadsAnalysis(ctx context.Context, branchID, actorID uuid.UUID, from, to time.Time) (map[string]any, error) {
	if s.lost == nil {
		return nil, shared.NewValidation("lost lead reader not wired")
	}
	var actor *uuid.UUID
	if actorID != uuid.Nil {
		actor = &actorID
	}
	st, p, key, err := s.resolve(ctx, branchID)
	if err != nil {
		return nil, err
	}
	leads, err := s.lost.LostBetween(ctx, branchID, from, to, lostLeadsLimit)
	if err != nil {
		return nil, err
	}
	reasons := countReasons(leads)
	out := map[string]any{
		"period_start": from.Format(time.RFC3339),
		"period_end":   to.Format(time.RFC3339),
		"lost_count":   len(leads),
		"reasons":      reasons,
		"ai_enabled":   true,
	}
	scope := map[string]any{"period_start": out["period_start"], "period_end": out["period_end"], "lost_count": len(leads)}
	hash := domain.HashInput("lost", branchID.String(), from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	finish := func(status string) (map[string]any, error) {
		run, _ := s.record(ctx, branchID, actor, domain.KindLostLeads, st, scope, out, hash, status, "")
		out["run_id"] = runID(run)
		if run != nil {
			out["created_at"] = run.CreatedAt.UTC().Format(time.RFC3339)
		}
		return out, nil
	}
	fail := func(msg string, appErr *shared.AppError) (map[string]any, error) {
		_, _ = s.record(ctx, branchID, actor, domain.KindLostLeads, st, scope, nil, hash, "error", msg)
		return nil, appErr
	}

	if len(leads) == 0 {
		out["source"] = "none"
		return finish("ok")
	}
	resp, err := s.complete(ctx, st, p, key, domain.CompletionRequest{
		System: lostLeadsSystem, User: lostLeadsPrompt(from, to, leads, reasons),
		JSONMode: true, MaxTokens: lostLeadsMaxToken,
	})
	if err != nil {
		return fail(trimErr(err), providerFailure(err, st.Model))
	}
	advice, ok := parseLostAdvice(resp.Text)
	if !ok {
		return fail("unusable response", shared.NewValidation(CodeProviderError+": unusable response"))
	}
	out["source"] = "ai"
	out["model"] = resp.Model
	out["summary"] = map[string]string{"en": advice.SummaryEN, "ar": advice.SummaryAR}
	out["actions"] = map[string][]string{"en": advice.ActionsEN, "ar": advice.ActionsAR}
	return finish("ok")
}

// LatestLostLeadsAnalysis returns the branch's newest successful analysis,
// or nil when none exists.
func (s *Service) LatestLostLeadsAnalysis(ctx context.Context, branchID uuid.UUID) (map[string]any, error) {
	return s.latestAIRun(ctx, branchID, domain.KindLostLeads, "ai", "none")
}

func countReasons(leads []LostLead) []ReasonCount {
	counts := map[string]int{}
	for _, l := range leads {
		code := l.ReasonCode
		if code == "" {
			code = "other"
		}
		counts[code]++
	}
	out := make([]ReasonCount, 0, len(counts))
	for code, n := range counts {
		out = append(out, ReasonCount{Code: code, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Code < out[j].Code
	})
	return out
}

func lostLeadsPrompt(from, to time.Time, leads []LostLead, reasons []ReasonCount) string {
	sample := leads
	if len(sample) > lostPromptLeads {
		sample = sample[:lostPromptLeads]
	}
	notes := make([]LostLead, 0, len(sample))
	for _, l := range sample {
		l.Note = truncateRunes(strings.TrimSpace(l.Note), lostNoteMaxRunes)
		notes = append(notes, l)
	}
	var b strings.Builder
	b.WriteString("Period: " + from.Format(time.DateOnly) + " to " + to.Add(-time.Second).Format(time.DateOnly) + "\n")
	b.WriteString("Lost leads: " + mustJSON(len(leads)) + "\n")
	b.WriteString("Reason counts: " + mustJSON(reasons) + "\n")
	b.WriteString("Lost leads (reason code, stage it was lost from, lead source, salesperson's note):\n")
	b.WriteString(mustJSON(notes))
	return b.String()
}

type lostAdvice struct {
	SummaryEN string   `json:"summary_en"`
	SummaryAR string   `json:"summary_ar"`
	ActionsEN []string `json:"actions_en"`
	ActionsAR []string `json:"actions_ar"`
}

func parseLostAdvice(text string) (lostAdvice, bool) {
	var a lostAdvice
	raw, _ := json.Marshal(parseJSONObject(text))
	if err := json.Unmarshal(raw, &a); err != nil {
		return lostAdvice{}, false
	}
	a.SummaryEN = truncateRunes(strings.TrimSpace(a.SummaryEN), lostTextMaxRunes*2)
	a.SummaryAR = truncateRunes(strings.TrimSpace(a.SummaryAR), lostTextMaxRunes*2)
	a.ActionsEN = cleanActions(a.ActionsEN)
	a.ActionsAR = cleanActions(a.ActionsAR)
	return a, a.SummaryEN != "" && len(a.ActionsEN) > 0
}

var listMarker = regexp.MustCompile(`^\s*(?:[-•*]|\d{1,2}[.)])\s*`)

func cleanActions(in []string) []string {
	out := make([]string, 0, lostMaxActions)
	for _, s := range in {
		s = strings.TrimSpace(listMarker.ReplaceAllString(s, ""))
		if s == "" {
			continue
		}
		out = append(out, truncateRunes(s, lostTextMaxRunes))
		if len(out) == lostMaxActions {
			break
		}
	}
	return out
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}
