package company

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct {
	companies map[uuid.UUID]domain.Company
	branches  []domain.Branch
	takenCode map[string]bool
}

func newMemRepo() *memRepo {
	return &memRepo{companies: map[uuid.UUID]domain.Company{}, takenCode: map[string]bool{}}
}

func (m *memRepo) CreateCompany(_ context.Context, c *domain.Company) error {
	for _, x := range m.companies {
		if x.Slug == c.Slug {
			err := shared.NewConflict("company exists")
			err.Details = map[string]any{"slug": "taken"}
			return err
		}
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	if c.PlanStartedAt.IsZero() {
		c.PlanStartedAt = c.CreatedAt
	}
	c.IsActive = true
	m.companies[c.ID] = *c
	return nil
}

func (m *memRepo) DeleteCompany(_ context.Context, id uuid.UUID, retiredSlug string) error {
	c, ok := m.companies[id]
	if !ok || !c.IsActive {
		return shared.NewNotFound("company")
	}
	c.IsActive = false
	c.Slug = retiredSlug
	m.companies[id] = c
	for i := range m.branches {
		if m.branches[i].CompanyID == id {
			m.branches[i].IsActive = false
		}
	}
	return nil
}

func (m *memRepo) GetCompany(_ context.Context, id uuid.UUID) (*domain.Company, error) {
	c, ok := m.companies[id]
	if !ok {
		return nil, shared.NewNotFound("company")
	}
	return &c, nil
}

func (m *memRepo) UpdateCompany(_ context.Context, c *domain.Company, _ uuid.UUID) error {
	m.companies[c.ID] = *c
	return nil
}

func (m *memRepo) ListCompanies(context.Context, string, int, int) (domain.Page, error) {
	return domain.Page{}, nil
}

func (m *memRepo) CreateBranch(_ context.Context, b *domain.Branch) error {
	if m.takenCode[b.Code] {
		err := shared.NewConflict("branch code exists")
		err.Details = map[string]any{"code": "taken"}
		return err
	}
	m.takenCode[b.Code] = true
	m.branches = append(m.branches, *b)
	return nil
}

func (m *memRepo) UpdateBranch(_ context.Context, b *domain.Branch) error {
	for i := range m.branches {
		if m.branches[i].ID == b.ID {
			m.branches[i] = *b
			return nil
		}
	}
	return shared.NewNotFound("branch")
}

func (m *memRepo) DemoteMainCenter(_ context.Context, companyID, keep uuid.UUID) error {
	for i := range m.branches {
		b := &m.branches[i]
		if b.CompanyID == companyID && b.ID != keep && b.Kind == domain.KindMainCenter {
			b.Kind = domain.KindBranch
		}
	}
	return nil
}

func (m *memRepo) ListBranches(_ context.Context, companyID uuid.UUID) ([]domain.Branch, error) {
	var out []domain.Branch
	for _, b := range m.branches {
		if b.CompanyID == companyID {
			out = append(out, b)
		}
	}
	return out, nil
}

func (m *memRepo) WorkspaceOf(context.Context, uuid.UUID) (*domain.Workspace, error) {
	return nil, shared.NewNotFound("branch")
}

func (m *memRepo) LockCompany(context.Context, uuid.UUID) error { return nil }

func (m *memRepo) UpdatePlan(_ context.Context, id uuid.UUID, plan domain.Plan, _ uuid.UUID) error {
	c, ok := m.companies[id]
	if !ok {
		return shared.NewNotFound("company")
	}
	c.Plan = plan
	c.PlanStartedAt = time.Now().UTC()
	m.companies[id] = c
	return nil
}

func (m *memRepo) CountUsage(_ context.Context, id uuid.UUID, kind domain.LimitKind) (int, error) {
	if kind == domain.LimitBranches {
		n := 0
		for _, b := range m.branches {
			if b.CompanyID == id {
				n++
			}
		}
		return n, nil
	}
	return 0, nil
}

func (m *memRepo) mainCenters(companyID uuid.UUID) int {
	n := 0
	for _, b := range m.branches {
		if b.CompanyID == companyID && b.Kind == domain.KindMainCenter {
			n++
		}
	}
	return n
}

type memUsers struct {
	gmBranch uuid.UUID
	fail     error
}

func (u *memUsers) CreateGM(_ context.Context, branchID uuid.UUID, _ GMAccount, _ Actor) (uuid.UUID, error) {
	if u.fail != nil {
		return uuid.Nil, u.fail
	}
	u.gmBranch = branchID
	return uuid.New(), nil
}

type nopAudit struct{}

func (nopAudit) Record(context.Context, audit.RecordInput) error { return nil }

type memCache struct{ n int }

func (c *memCache) Invalidate(uuid.UUID) { c.n++ }

func registration() RegisterInput {
	return RegisterInput{
		Company: domain.Company{NameEN: "Al Noor Travel", Plan: domain.PlanUltimate},
		GM:      GMAccount{FullName: "Sara GM", Email: "Sara@AlNoor.test", Password: "Str0ngPassw0rd!"},
	}
}

func platformCtx() context.Context {
	return access.WithScope(context.Background(), access.System())
}

func companyCtx(companyID uuid.UUID, branches ...uuid.UUID) context.Context {
	return access.WithScope(context.Background(), access.Scope{
		Level: access.LevelCompany, CompanyID: companyID, BranchID: branches[0], Branches: branches,
	})
}

func TestRegisterCreatesMainCenterAndGM(t *testing.T) {
	repo, users, cache := newMemRepo(), &memUsers{}, &memCache{}
	svc := NewService(repo, users, tx.Nop{}, nopAudit{}, cache)
	out, err := svc.Register(platformCtx(), registration())
	if err != nil {
		t.Fatal(err)
	}
	if out.Company.Slug != "al-noor-travel" || out.MainCenter.Kind != domain.KindMainCenter ||
		out.MainCenter.Slug != domain.MainCenterSlug || out.MainCenter.CompanyID != out.Company.ID {
		t.Fatalf("registered: %+v", out)
	}
	if users.gmBranch != out.MainCenter.ID {
		t.Fatal("the GM must belong to the main center")
	}
}

func TestRegisterReportsEveryInvalidField(t *testing.T) {
	svc := NewService(newMemRepo(), &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	_, err := svc.Register(platformCtx(), RegisterInput{GM: GMAccount{Email: "nope", Password: "short"}})
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want validation, got %v", err)
	}
	for _, f := range []string{"name_en", "gm_full_name", "gm_email", "gm_password"} {
		if _, ok := app.Details[f]; !ok {
			t.Errorf("missing %q in %v", f, app.Details)
		}
	}
}

func TestRegisterSurfacesGMFailure(t *testing.T) {
	conflict := shared.NewConflict("gm email already in use")
	svc := NewService(newMemRepo(), &memUsers{fail: conflict}, tx.Nop{}, nopAudit{}, &memCache{})
	if _, err := svc.Register(platformCtx(), registration()); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestMainCenterCodeCollisionGetsSuffix(t *testing.T) {
	repo := newMemRepo()
	repo.takenCode["ALNOORTRAV"] = true
	svc := NewService(repo, &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	out, err := svc.Register(platformCtx(), registration())
	if err != nil {
		t.Fatal(err)
	}
	if out.MainCenter.Code == "ALNOORTRAV" {
		t.Fatalf("code must be made unique, got %s", out.MainCenter.Code)
	}
}

func TestBranchLifecycleKeepsExactlyOneMainCenter(t *testing.T) {
	repo, cache := newMemRepo(), &memCache{}
	svc := NewService(repo, &memUsers{}, tx.Nop{}, nopAudit{}, cache)
	reg, err := svc.Register(platformCtx(), registration())
	if err != nil {
		t.Fatal(err)
	}
	cid, main := reg.Company.ID, reg.MainCenter.ID
	ctx := companyCtx(cid, main)

	jeddah, err := svc.CreateBranch(ctx, BranchInput{NameEN: "Jeddah Office"})
	if err != nil {
		t.Fatal(err)
	}
	if jeddah.Kind != domain.KindBranch || jeddah.Slug != "jeddah-office" || jeddah.Code == "" {
		t.Fatalf("new branch: %+v", jeddah)
	}
	if repo.mainCenters(cid) != 1 {
		t.Fatal("adding a branch must not touch the main center")
	}

	promote := domain.KindMainCenter
	if _, err := svc.UpdateBranch(ctx, jeddah.ID, BranchPatch{Kind: &promote}); err != nil {
		t.Fatal(err)
	}
	if repo.mainCenters(cid) != 1 {
		t.Fatalf("promotion must demote the old main center, have %d", repo.mainCenters(cid))
	}

	demote := domain.KindBranch
	if _, err := svc.UpdateBranch(ctx, jeddah.ID, BranchPatch{Kind: &demote}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("demoting the only main center must fail, got %v", err)
	}
	if cache.n < 2 {
		t.Fatal("branch changes must refresh the tenant cache")
	}
}

func TestStandardPlanRefusesASecondBranch(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	in := registration()
	in.Company.Plan = domain.PlanStandard
	reg, err := svc.Register(platformCtx(), in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateBranch(companyCtx(reg.Company.ID, reg.MainCenter.ID), BranchInput{NameEN: "Jeddah"}); err == nil {
		t.Fatal("standard is one branch")
	}
}

func TestBranchCallsNeedCompanyScope(t *testing.T) {
	svc := NewService(newMemRepo(), &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelBranch, BranchID: uuid.New()})
	if _, err := svc.CreateBranch(ctx, BranchInput{NameEN: "X"}); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("want forbidden without a company, got %v", err)
	}
}

func TestPlatformSaveKeepsThePackageYear(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	reg, err := svc.Register(platformCtx(), registration())
	if err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := repo.companies[reg.Company.ID]
	c.Plan, c.PlanStartedAt = domain.PlanPremium, started
	repo.companies[c.ID] = c

	got, err := svc.SaveCompany(platformCtx(), c.ID, domain.Company{NameEN: "Al Noor Desk"}, Actor{})
	if err != nil {
		t.Fatal(err)
	}
	if got.NameEN != "Al Noor Desk" || got.Plan != domain.PlanPremium || !got.PlanStartedAt.Equal(started) {
		t.Fatalf("rename must not reset the year: %+v", got)
	}
}

func TestUpdatePlanOpensANewYear(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	reg, err := svc.Register(platformCtx(), registration())
	if err != nil {
		t.Fatal(err)
	}
	c := repo.companies[reg.Company.ID]
	c.PlanStartedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.companies[c.ID] = c
	if _, err := svc.UpdatePlan(platformCtx(), c.ID, domain.PlanPremium, Actor{}); err != nil {
		t.Fatal(err)
	}
	next := repo.companies[c.ID]
	if next.Plan != domain.PlanPremium || next.PlanStartedAt.Before(time.Now().UTC().Add(-time.Minute)) {
		t.Fatalf("new year: %+v", next)
	}
}

func TestDeleteRetiresSlugAndKicksStaff(t *testing.T) {
	repo, cache := newMemRepo(), &memCache{}
	svc := NewService(repo, &memUsers{}, tx.Nop{}, nopAudit{}, cache)
	reg, err := svc.Register(platformCtx(), registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(platformCtx(), reg.Company.ID, reg.Company.Slug, Actor{}); err != nil {
		t.Fatal(err)
	}
	got := repo.companies[reg.Company.ID]
	if got.IsActive || got.Slug != domain.RetiredSlug(reg.Company.ID) {
		t.Fatalf("retired: %+v", got)
	}
	if repo.branches[0].IsActive {
		t.Fatal("branches must close with the company")
	}
	if cache.n < 1 {
		t.Fatal("delete must drop the tenant cache")
	}
	if err := svc.Delete(platformCtx(), reg.Company.ID, "al-noor-travel", Actor{}); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := svc.SaveCompany(platformCtx(), reg.Company.ID, domain.Company{NameEN: "Nope"}, Actor{}); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("save after delete: %v", err)
	}
}

func TestDeleteRequiresTheLiveSlug(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	reg, err := svc.Register(platformCtx(), registration())
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(platformCtx(), reg.Company.ID, "wrong-slug", Actor{}); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want slug confirmation, got %v", err)
	}
	if !repo.companies[reg.Company.ID].IsActive {
		t.Fatal("a mistyped confirm must not retire the tenant")
	}
}

func TestDeleteNeedsPlatformScope(t *testing.T) {
	svc := NewService(newMemRepo(), &memUsers{}, tx.Nop{}, nopAudit{}, &memCache{})
	ctx := companyCtx(uuid.New(), uuid.New())
	if err := svc.Delete(ctx, uuid.New(), "x", Actor{}); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("want forbidden, got %v", err)
	}
}
