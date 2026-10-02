package branding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type memRepo struct {
	slug  string
	id    uuid.UUID
	logo  *domain.Logo
	slugs []string
}

func (m *memRepo) branding() *domain.Branding {
	b := &domain.Branding{Slug: m.slug, NameEN: "Acme"}
	if m.logo != nil {
		t := m.logo.UpdatedAt
		b.LogoUpdatedAt = &t
	}
	return b
}

func (m *memRepo) BrandingBySlug(_ context.Context, slug string) (*domain.Branding, error) {
	m.slugs = append(m.slugs, slug)
	if slug != m.slug {
		return nil, shared.NewNotFound("company")
	}
	return m.branding(), nil
}

func (m *memRepo) LogoBySlug(_ context.Context, slug string) (*domain.Logo, error) {
	if slug != m.slug || m.logo == nil {
		return nil, shared.NewNotFound("logo")
	}
	return m.logo, nil
}

func (m *memRepo) SetLogo(_ context.Context, id uuid.UUID, l domain.Logo) error {
	if id != m.id {
		return shared.NewNotFound("company")
	}
	m.logo = &l
	return nil
}

func (m *memRepo) DeleteLogo(_ context.Context, id uuid.UUID) error {
	if id != m.id {
		return shared.NewNotFound("company")
	}
	m.logo = nil
	return nil
}

func (m *memRepo) BrandingOf(_ context.Context, _ uuid.UUID) (*domain.Branding, error) {
	return m.branding(), nil
}

func TestBranding(t *testing.T) {
	ctx := context.Background()
	repo := &memRepo{slug: "acme", id: uuid.New()}
	svc := NewService(repo, nil)
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	actor := Actor{CompanyID: repo.id}

	if b, err := svc.Branding(ctx, " ACME "); err != nil || b.Slug != "acme" || b.LogoUpdatedAt != nil {
		t.Fatalf("lookup normalizes the slug: %+v %v", b, err)
	}
	for _, bad := range []string{"", "login", "../etc", "a", "Acme Travel"} {
		if _, err := svc.Branding(ctx, bad); !errors.Is(err, shared.ErrNotFound) {
			t.Errorf("%q: want not found, got %v", bad, err)
		}
	}
	if len(repo.slugs) != 1 {
		t.Fatalf("invalid slugs must never reach the store: %v", repo.slugs)
	}

	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	b, err := svc.SetLogo(ctx, actor, png)
	if err != nil || b.LogoUpdatedAt == nil || !b.LogoUpdatedAt.Equal(now) {
		t.Fatalf("upload: %+v %v", b, err)
	}
	if l, err := svc.Logo(ctx, "acme"); err != nil || l.ContentType != "image/png" {
		t.Fatalf("public logo: %+v %v", l, err)
	}
	if _, err := svc.SetLogo(ctx, actor, []byte("<svg/>")); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("svg must be rejected: %v", err)
	}

	if b, err = svc.DeleteLogo(ctx, actor); err != nil || b.LogoUpdatedAt != nil {
		t.Fatalf("delete: %+v %v", b, err)
	}
	if _, err := svc.Logo(ctx, "acme"); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("deleted logo: %v", err)
	}
}

type memAudit struct{ records []audit.RecordInput }

func (m *memAudit) Record(_ context.Context, in audit.RecordInput) error {
	m.records = append(m.records, in)
	return nil
}

func TestLogoAuditBranch(t *testing.T) {
	ctx := context.Background()
	repo := &memRepo{slug: "acme", id: uuid.New()}
	rec := &memAudit{}
	svc := NewService(repo, rec)
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

	if _, err := svc.SetLogo(ctx, Actor{CompanyID: repo.id, UserID: uuid.New()}, png); err != nil {
		t.Fatal(err)
	}
	branch := uuid.New()
	if _, err := svc.DeleteLogo(ctx, Actor{CompanyID: repo.id, BranchID: branch}); err != nil {
		t.Fatal(err)
	}
	if len(rec.records) != 2 {
		t.Fatalf("want 2 audit records, got %d", len(rec.records))
	}
	if rec.records[0].BranchID != nil {
		t.Errorf("platform admin upload has no branch, got %v", *rec.records[0].BranchID)
	}
	if got := rec.records[1].BranchID; got == nil || *got != branch {
		t.Errorf("GM removal keeps its branch, got %v", got)
	}
	if rec.records[0].EntityID == nil || *rec.records[0].EntityID != repo.id {
		t.Errorf("audit entity is the company")
	}
}
