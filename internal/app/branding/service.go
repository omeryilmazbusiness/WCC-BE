// Package branding serves a company's public sign-in identity and lets its GM
// manage the logo.
package branding

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Actor changes a company's logo: its GM, or a platform admin (no branch).
type Actor struct {
	CompanyID uuid.UUID
	BranchID  uuid.UUID
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

type Service struct {
	repo  domain.BrandingRepository
	audit audit.Recorder
	now   func() time.Time
}

func NewService(repo domain.BrandingRepository, rec audit.Recorder) *Service {
	return &Service{repo: repo, audit: rec, now: func() time.Time { return time.Now().UTC() }}
}

// slugOf rejects anything that is not a possible company slug before it reaches the database.
func slugOf(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if !domain.ValidCompanySlug(slug) {
		return "", shared.NewNotFound("company")
	}
	return slug, nil
}

func (s *Service) Branding(ctx context.Context, slug string) (*domain.Branding, error) {
	clean, err := slugOf(slug)
	if err != nil {
		return nil, err
	}
	return s.repo.BrandingBySlug(ctx, clean)
}

func (s *Service) Logo(ctx context.Context, slug string) (*domain.Logo, error) {
	clean, err := slugOf(slug)
	if err != nil {
		return nil, err
	}
	return s.repo.LogoBySlug(ctx, clean)
}

func (s *Service) SetLogo(ctx context.Context, a Actor, data []byte) (*domain.Branding, error) {
	logo, err := domain.NewLogo(data)
	if err != nil {
		return nil, err
	}
	logo.UpdatedAt = s.now()
	if err := s.repo.SetLogo(ctx, a.CompanyID, logo); err != nil {
		return nil, err
	}
	s.record(ctx, a, "company.logo_updated", map[string]any{"content_type": logo.ContentType, "bytes": len(logo.Data)})
	return s.repo.BrandingOf(ctx, a.CompanyID)
}

func (s *Service) DeleteLogo(ctx context.Context, a Actor) (*domain.Branding, error) {
	if err := s.repo.DeleteLogo(ctx, a.CompanyID); err != nil {
		return nil, err
	}
	s.record(ctx, a, "company.logo_removed", nil)
	return s.repo.BrandingOf(ctx, a.CompanyID)
}

func (s *Service) record(ctx context.Context, a Actor, action string, extra map[string]any) {
	if s.audit == nil {
		return
	}
	id := a.CompanyID
	in := audit.RecordInput{
		ActorID: a.UserID, Action: action, EntityType: "company", EntityID: &id,
		IP: a.IP, UserAgent: a.UserAgent, Extra: extra,
	}
	if a.BranchID != uuid.Nil {
		branch := a.BranchID
		in.BranchID = &branch
	}
	_ = s.audit.Record(ctx, in)
}
