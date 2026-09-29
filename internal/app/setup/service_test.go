package setup

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/setup"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct {
	states map[uuid.UUID]domain.State
}

func (m *memRepo) Lock(context.Context, uuid.UUID) error { return nil }

func (m *memRepo) GetState(_ context.Context, id uuid.UUID) (*domain.State, error) {
	s := m.states[id]
	s.CompanyID = id
	return &s, nil
}

func (m *memRepo) SaveState(_ context.Context, s *domain.State) error {
	m.states[s.CompanyID] = *s
	return nil
}

type memCompanies struct {
	companies map[uuid.UUID]company.Company
	branches  []company.Branch
	failSave  bool
}

func (m *memCompanies) GetCompany(_ context.Context, id uuid.UUID) (*company.Company, error) {
	c, ok := m.companies[id]
	if !ok {
		return nil, shared.NewNotFound("company")
	}
	return &c, nil
}

func (m *memCompanies) UpdateCompany(_ context.Context, c *company.Company, _ uuid.UUID) error {
	if m.failSave {
		return errors.New("db down")
	}
	m.companies[c.ID] = *c
	return nil
}

func (m *memCompanies) ListBranches(context.Context, uuid.UUID) ([]company.Branch, error) {
	return m.branches, nil
}

type fixedFacts struct {
	staff        int
	ai           bool
	channels     int
	staffBranchN int
}

func (f *fixedFacts) CountStaff(_ context.Context, ids []uuid.UUID) (int, error) {
	f.staffBranchN = len(ids)
	return f.staff, nil
}
func (f *fixedFacts) AIReady(context.Context, uuid.UUID) (bool, error) { return f.ai, nil }
func (f *fixedFacts) ConnectedChannels(context.Context, uuid.UUID) (int, error) {
	return f.channels, nil
}

type memAudit struct{ actions []string }

func (a *memAudit) Record(_ context.Context, in audit.RecordInput) error {
	a.actions = append(a.actions, in.Action)
	return nil
}

type memCache struct{ invalidated []uuid.UUID }

func (c *memCache) Invalidate(id uuid.UUID) { c.invalidated = append(c.invalidated, id) }

type fixture struct {
	svc       *Service
	repo      *memRepo
	companies *memCompanies
	facts     *fixedFacts
	audit     *memAudit
	cache     *memCache
	actor     Actor
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	companyID, main, second := uuid.New(), uuid.New(), uuid.New()
	f := fixture{
		repo: &memRepo{states: map[uuid.UUID]domain.State{}},
		companies: &memCompanies{
			companies: map[uuid.UUID]company.Company{companyID: {
				ID: companyID, Slug: "old-co", NameEN: "Old", Currency: "USD", Timezone: "Asia/Riyadh", IsActive: true,
			}},
			branches: []company.Branch{
				{ID: main, CompanyID: companyID, Slug: "main", Kind: company.KindMainCenter},
				{ID: second, CompanyID: companyID, Slug: "jeddah", Kind: company.KindBranch},
			},
		},
		facts: &fixedFacts{}, audit: &memAudit{}, cache: &memCache{},
		actor: Actor{CompanyID: companyID, BranchID: main, UserID: uuid.New()},
	}
	f.svc = NewService(Deps{
		Repo: f.repo, Companies: f.companies, Staff: f.facts, AI: f.facts, Channels: f.facts,
		Tx: tx.Nop{}, Audit: f.audit, Caches: f.cache,
	})
	return f
}

func fullProfile() company.Company {
	return company.Company{
		NameEN: "WODI Travel", Email: "hi@wodi.com", Country: "SA", City: "Riyadh", Address: "King Fahd Rd 1",
	}
}

func TestOverviewFreshCompany(t *testing.T) {
	f := newFixture(t)
	o, err := f.svc.Overview(context.Background(), f.actor)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Progress.Required || o.Progress.Next != domain.StepCompany || o.Company.NameEN != "Old" || len(o.Branches) != 2 {
		t.Fatalf("overview: %+v", o)
	}
	if f.facts.staffBranchN != 2 {
		t.Fatalf("staff must be counted company-wide, got %d branches", f.facts.staffBranchN)
	}
}

func TestFullOnboardingFlow(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	o, err := f.svc.SaveCompany(ctx, f.actor, fullProfile())
	if err != nil {
		t.Fatalf("save company: %v", err)
	}
	saved := f.companies.companies[f.actor.CompanyID]
	if o.Progress.Next != domain.StepStaff || saved.NameEN != "WODI Travel" || saved.Slug != "wodi-travel" {
		t.Fatalf("after company (renaming re-derives the URL): %+v %+v", o.Progress, saved)
	}
	if len(f.cache.invalidated) != 1 {
		t.Fatal("saving the profile must refresh the tenant cache")
	}

	f.facts.staff = 2
	if o, err = f.svc.Advance(ctx, f.actor, domain.StepStaff, false); err != nil || o.Progress.Next != domain.StepAI {
		t.Fatalf("staff: %v %+v", err, o)
	}
	if _, err = f.svc.Advance(ctx, f.actor, domain.StepAI, false); err == nil {
		t.Fatal("ai must be configured or skipped")
	}
	f.facts.ai = true
	if o, err = f.svc.Advance(ctx, f.actor, domain.StepAI, false); err != nil || o.Progress.Next != domain.StepChannels {
		t.Fatalf("ai: %v %+v", err, o)
	}
	if o, err = f.svc.Advance(ctx, f.actor, domain.StepChannels, true); err != nil {
		t.Fatalf("channels skip: %v", err)
	}
	if o, err = f.svc.Complete(ctx, f.actor); err != nil || !o.Progress.Completed || o.Progress.Required || o.Progress.FullyDone {
		t.Fatalf("complete with skipped channels stays open on the dashboard: %v %+v", err, o)
	}

	want := []string{"setup.company_saved", "setup.step_advanced", "setup.step_advanced", "setup.step_advanced", "setup.completed"}
	if len(f.audit.actions) != len(want) {
		t.Fatalf("audit actions: %v", f.audit.actions)
	}
	for i := range want {
		if f.audit.actions[i] != want[i] {
			t.Fatalf("audit actions: %v", f.audit.actions)
		}
	}
}

func TestCompanyProfileRequiresLocation(t *testing.T) {
	f := newFixture(t)
	_, err := f.svc.SaveCompany(context.Background(), f.actor, company.Company{NameEN: "WODI"})
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation, got %v", err)
	}
	for _, field := range []string{"country", "city", "address"} {
		if _, ok := app.Details[field]; !ok {
			t.Errorf("missing field error %q in %v", field, app.Details)
		}
	}
	if f.companies.companies[f.actor.CompanyID].NameEN != "Old" || len(f.audit.actions) != 0 {
		t.Fatal("invalid profile must not be saved or audited")
	}
}

func TestCompanySaveFailureLeavesStepPending(t *testing.T) {
	f := newFixture(t)
	f.companies.failSave = true
	if _, err := f.svc.SaveCompany(context.Background(), f.actor, fullProfile()); err == nil {
		t.Fatal("expected save error")
	}
	if f.repo.states[f.actor.CompanyID].CompanyDoneAt != nil || len(f.cache.invalidated) != 0 {
		t.Fatal("company step marked done despite failed save")
	}
}

func TestDismissAndCompleteGuards(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.Complete(ctx, f.actor); err == nil {
		t.Fatal("complete requires company")
	}
	o, err := f.svc.Dismiss(ctx, f.actor)
	if err != nil || o.Progress.Required {
		t.Fatalf("dismiss: %v %+v", err, o)
	}
}

func TestCompanySlugFollowsEnglishName(t *testing.T) {
	cases := []struct {
		name, slug, want string
	}{
		{"Old", "", "old-co"},
		{"  Old  ", "", "old-co"},
		{"Test", "", "test"},
		{"شركة وفاد", "", "old-co"},
		{"Test", "custom-url", "custom-url"},
	}
	for _, tc := range cases {
		f := newFixture(t)
		p := fullProfile()
		p.NameEN, p.Slug = tc.name, tc.slug
		if _, err := f.svc.SaveCompany(context.Background(), f.actor, p); err != nil {
			t.Fatalf("%q: %v", tc.name, err)
		}
		if got := f.companies.companies[f.actor.CompanyID].Slug; got != tc.want {
			t.Errorf("name %q slug %q: got %q, want %q", tc.name, tc.slug, got, tc.want)
		}
	}
}
