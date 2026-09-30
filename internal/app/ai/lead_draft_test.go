package ai_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/ai"
)

type draftReader struct{ conv ai.DraftConversation }

func (r draftReader) DraftConversation(context.Context, uuid.UUID, uuid.UUID, int) (*ai.DraftConversation, error) {
	c := r.conv
	return &c, nil
}

type catalog []ai.CatalogPackage

func (c catalog) ActivePackages(context.Context, uuid.UUID) ([]ai.CatalogPackage, error) {
	return c, nil
}

var umrah = ai.CatalogPackage{ID: uuid.New(), Code: "UMR-10", Name: "Umrah 10 nights"}

func draftFixture(t *testing.T, withAI bool, p *fakeProvider, lines []string) (*ai.Service, *runRepo, uuid.UUID) {
	t.Helper()
	svc, repo, branch := lostFixture(t, withAI, p, nil)
	svc.SetDraftConversationReader(draftReader{conv: ai.DraftConversation{
		ContactName: "Abu Ahmad", ContactPhone: "+966500000001", Lines: lines,
	}})
	svc.SetPackageCatalog(catalog{umrah})
	return svc, repo, branch
}

var chat = []string{
	"customer: Salam, I am Ahmed. We are 4 people and want Umrah UMR-10 around 10 March 2027.",
	"agent: Welcome Ahmed! What is your budget?",
	"customer: About 12500 riyal in total.",
}

func TestLeadDraftFillsTheFormFromTheChat(t *testing.T) {
	p := &fakeProvider{reply: `{"full_name":"Ahmed","phone":"+10000000000","travel_date":"2027-03-10","pax_count":4,
		"budget_amount":12500,"budget_currency":"SAR","package_code":"UMR-10","package_interest":null,"notes":null}`}
	svc, repo, branch := draftFixture(t, true, p, chat)
	out, err := svc.LeadDraftFromConversation(context.Background(), branch, uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	d := out["draft"].(ai.LeadDraft)
	if d.FullName != "Ahmed" || d.Phone != "+966500000001" || *d.PaxCount != 4 || *d.BudgetAmount != 1250000 || *d.PackageID != umrah.ID {
		t.Fatalf("draft: %+v", d)
	}
	for _, want := range []string{"UMR-10: Umrah 10 nights", "customer: About 12500 riyal", "Today: "} {
		if !strings.Contains(p.req.User, want) {
			t.Fatalf("prompt missing %q:\n%s", want, p.req.User)
		}
	}
	if len(repo.runs) != 1 || repo.runs[0].Kind != domain.KindLeadDraft || repo.runs[0].Status != "ok" {
		t.Fatalf("runs: %+v", repo.runs)
	}
}

func TestLeadDraftNeedsAIAndACustomerMessage(t *testing.T) {
	svc, repo, branch := draftFixture(t, false, nil, chat)
	if _, err := svc.LeadDraftFromConversation(context.Background(), branch, uuid.New(), uuid.New()); !domain.IsNotConfigured(err) {
		t.Fatalf("err = %v, want ai_not_configured", err)
	}
	p := &fakeProvider{reply: "{}"}
	svc, _, branch = draftFixture(t, true, p, []string{"agent: Hello, how can we help?"})
	if _, err := svc.LeadDraftFromConversation(context.Background(), branch, uuid.New(), uuid.New()); err == nil || p.req.User != "" {
		t.Fatalf("a chat without customer messages must not reach the model: %v", err)
	}
	if len(repo.runs) != 0 {
		t.Fatalf("runs: %+v", repo.runs)
	}
}

func TestLeadDraftSurfacesProviderFailure(t *testing.T) {
	p := &fakeProvider{err: errors.New("rate limited")}
	svc, repo, branch := draftFixture(t, true, p, chat)
	_, err := svc.LeadDraftFromConversation(context.Background(), branch, uuid.New(), uuid.New())
	if err == nil || !strings.Contains(err.Error(), "ai_provider_error") {
		t.Fatalf("err = %v", err)
	}
	if len(repo.runs) != 1 || repo.runs[0].Status != "error" {
		t.Fatalf("failed run must be recorded: %+v", repo.runs)
	}
}
