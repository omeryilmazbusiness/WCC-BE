package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
)

type runRepo struct {
	settingsRepo
	runs []domain.Run
}

func (r *runRepo) InsertRun(_ context.Context, run *domain.Run) error {
	r.runs = append([]domain.Run{*run}, r.runs...)
	return nil
}

func (r *runRepo) ListRuns(_ context.Context, _ uuid.UUID, kind domain.Kind, status string, limit int) ([]domain.Run, error) {
	var out []domain.Run
	for _, run := range r.runs {
		if run.Kind == kind && (status == "" || run.Status == status) && len(out) < limit {
			out = append(out, run)
		}
	}
	return out, nil
}

type lostReader struct{ leads []ai.LostLead }

func (l lostReader) LostBetween(context.Context, uuid.UUID, time.Time, time.Time, int) ([]ai.LostLead, error) {
	return l.leads, nil
}

type fakeProvider struct {
	reply string
	err   error
	req   domain.CompletionRequest
}

func (p *fakeProvider) Name() domain.Provider { return domain.ProviderOpenAI }
func (p *fakeProvider) Complete(_ context.Context, _ string, req domain.CompletionRequest) (*domain.CompletionResponse, error) {
	p.req = req
	if p.err != nil {
		return nil, p.err
	}
	return &domain.CompletionResponse{Text: p.reply, Model: "gpt-test"}, nil
}

type registry struct{ p domain.CompletionProvider }

func (r registry) Get(domain.Provider) (domain.CompletionProvider, bool) { return r.p, r.p != nil }

var lostSample = []ai.LostLead{
	{ReasonCode: "price", Note: "Found a cheaper package at another agency", Source: "whatsapp", FromStage: "proposal"},
	{ReasonCode: "price", Note: "Budget too tight this season", Source: "instagram", FromStage: "qualified"},
	{ReasonCode: "no_response", Source: "website", FromStage: "contacted"},
}

func lostFixture(t *testing.T, withAI bool, p *fakeProvider, leads []ai.LostLead) (*ai.Service, *runRepo, uuid.UUID) {
	t.Helper()
	branch := uuid.New()
	repo := &runRepo{settingsRepo: settingsRepo{rows: map[uuid.UUID]domain.Settings{}}}
	if withAI {
		cfg, _ := json.Marshal(map[string]string{"api_key": "sk-test-1234"})
		repo.rows[branch] = domain.Settings{BranchID: branch, Provider: domain.ProviderOpenAI, Model: "gpt-test", Enabled: true, ConfigJSON: cfg}
	}
	svc := ai.NewService(repo, registry{p: p}, testBox(t))
	svc.SetLostLeadReader(lostReader{leads: leads})
	return svc, repo, branch
}

var (
	weekFrom = time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	weekTo   = weekFrom.AddDate(0, 0, 7)
)

func TestLostLeadsAnalysisSummarizesWithAI(t *testing.T) {
	p := &fakeProvider{reply: "```json\n" + `{
		"summary_en": "Most leads were lost on price; some never answered.",
		"summary_ar": "خسرنا معظم العملاء بسبب السعر.",
		"actions_en": ["1. Offer an installment plan in the first call", "- Follow up within 24 hours", "", "24 hour callback for every quote"],
		"actions_ar": ["اعرض خطة تقسيط في أول مكالمة"]
	}` + "\n```"}
	svc, repo, branch := lostFixture(t, true, p, lostSample)
	out, err := svc.LostLeadsAnalysis(context.Background(), branch, uuid.New(), weekFrom, weekTo)
	if err != nil {
		t.Fatal(err)
	}
	if out["source"] != "ai" || out["lost_count"] != 3 {
		t.Fatalf("out: %v", out)
	}
	reasons := out["reasons"].([]ai.ReasonCount)
	if reasons[0] != (ai.ReasonCount{Code: "price", Count: 2}) || reasons[1].Code != "no_response" {
		t.Fatalf("reasons: %+v", reasons)
	}
	actions := out["actions"].(map[string][]string)["en"]
	want := []string{"Offer an installment plan in the first call", "Follow up within 24 hours", "24 hour callback for every quote"}
	if strings.Join(actions, "|") != strings.Join(want, "|") {
		t.Fatalf("actions: %q", actions)
	}
	if !strings.Contains(p.req.User, "Period: 2026-09-21 to 2026-09-27") || !strings.Contains(p.req.User, "cheaper package") || !p.req.JSONMode {
		t.Fatalf("prompt: %s", p.req.User)
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != "ok" || repo.runs[0].Kind != domain.KindLostLeads {
		t.Fatalf("runs: %+v", repo.runs)
	}
}

func TestLostLeadsAnalysisNeedsAI(t *testing.T) {
	svc, repo, branch := lostFixture(t, false, nil, lostSample)
	_, err := svc.LostLeadsAnalysis(context.Background(), branch, uuid.Nil, weekFrom, weekTo)
	if !domain.IsNotConfigured(err) {
		t.Fatalf("err = %v, want ai_not_configured", err)
	}
	if len(repo.runs) != 0 {
		t.Fatalf("no run may be recorded without AI: %+v", repo.runs)
	}
	if ok, err := svc.Configured(context.Background(), branch); ok || err != nil {
		t.Fatalf("Configured = %v %v", ok, err)
	}
}

func TestLostLeadsAnalysisWithNothingLostSkipsTheModel(t *testing.T) {
	p := &fakeProvider{reply: "{}"}
	svc, _, branch := lostFixture(t, true, p, nil)
	out, err := svc.LostLeadsAnalysis(context.Background(), branch, uuid.Nil, weekFrom, weekTo)
	if err != nil || out["source"] != "none" || out["lost_count"] != 0 || p.req.User != "" {
		t.Fatalf("out: %v err: %v prompt: %q", out, err, p.req.User)
	}
}

func TestLatestLostLeadsAnalysisIgnoresFailedRuns(t *testing.T) {
	p := &fakeProvider{reply: `{"summary_en":"Price.","actions_en":["Offer installments"]}`}
	svc, _, branch := lostFixture(t, true, p, lostSample)
	ctx := context.Background()
	if _, err := svc.LostLeadsAnalysis(ctx, branch, uuid.Nil, weekFrom, weekTo); err != nil {
		t.Fatal(err)
	}
	p.err = errors.New("provider down")
	if out, err := svc.LostLeadsAnalysis(ctx, branch, uuid.New(), weekFrom, weekTo); err == nil || out != nil {
		t.Fatalf("provider failure must surface: %v %v", out, err)
	}
	p.err, p.reply = nil, `{"text":"not the shape"}`
	if _, err := svc.LostLeadsAnalysis(ctx, branch, uuid.New(), weekFrom, weekTo); err == nil {
		t.Fatal("unusable reply must be an error")
	}
	latest, err := svc.LatestLostLeadsAnalysis(ctx, branch)
	if err != nil || latest["source"] != "ai" || latest["run_id"] == "" {
		t.Fatalf("latest: %v %v", latest, err)
	}
	summary := latest["summary"].(map[string]any)
	if summary["en"] != "Price." {
		t.Fatalf("summary: %v", summary)
	}
}
