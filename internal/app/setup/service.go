// Package setup runs the GM first-run onboarding use cases of a company.
package setup

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/setup"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// CompanyStore reads and saves the company profile.
type CompanyStore interface {
	GetCompany(ctx context.Context, id uuid.UUID) (*company.Company, error)
	UpdateCompany(ctx context.Context, c *company.Company, actor uuid.UUID) error
	ListBranches(ctx context.Context, companyID uuid.UUID) ([]company.Branch, error)
}

// StaffCounter counts active staff (everyone but GMs and platform admins)
// across the given branches.
type StaffCounter interface {
	CountStaff(ctx context.Context, branchIDs []uuid.UUID) (int, error)
}

// AIStatus reports whether the branch has a usable AI provider.
type AIStatus interface {
	AIReady(ctx context.Context, branchID uuid.UUID) (bool, error)
}

// ChannelStatus counts connected messaging channels of a branch.
type ChannelStatus interface {
	ConnectedChannels(ctx context.Context, branchID uuid.UUID) (int, error)
}

// Invalidator drops cached tenant lookups after the company slug changes.
type Invalidator interface {
	Invalidate(companyID uuid.UUID)
}

type Service struct {
	repo      domain.Repository
	companies CompanyStore
	staff     StaffCounter
	ai        AIStatus
	channels  ChannelStatus
	tx        tx.Runner
	audit     audit.Recorder
	caches    Invalidator
	now       func() time.Time
}

type Deps struct {
	Repo      domain.Repository
	Companies CompanyStore
	Staff     StaffCounter
	AI        AIStatus
	Channels  ChannelStatus
	Tx        tx.Runner
	Audit     audit.Recorder
	Caches    Invalidator
}

func NewService(d Deps) *Service {
	return &Service{
		repo: d.Repo, companies: d.Companies, staff: d.Staff, ai: d.AI, channels: d.Channels,
		tx: d.Tx, audit: d.Audit, caches: d.Caches,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Actor identifies who acts for which company, from which branch (AI and
// channels are configured per branch), for scoping and the audit trail.
type Actor struct {
	CompanyID uuid.UUID
	BranchID  uuid.UUID
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

type Overview struct {
	Company  company.Company
	Branches []company.Branch
	State    domain.State
	Facts    domain.Facts
	Progress domain.Progress
}

func (s *Service) facts(ctx context.Context, a Actor, branches []company.Branch) (domain.Facts, error) {
	ids := make([]uuid.UUID, 0, len(branches))
	for _, b := range branches {
		ids = append(ids, b.ID)
	}
	staff, err := s.staff.CountStaff(ctx, ids)
	if err != nil {
		return domain.Facts{}, err
	}
	ai, err := s.ai.AIReady(ctx, a.BranchID)
	if err != nil {
		return domain.Facts{}, err
	}
	channels, err := s.channels.ConnectedChannels(ctx, a.BranchID)
	if err != nil {
		return domain.Facts{}, err
	}
	return domain.Facts{StaffCount: staff, AIReady: ai, ChannelsConnected: channels}, nil
}

func (s *Service) Overview(ctx context.Context, a Actor) (*Overview, error) {
	c, err := s.companies.GetCompany(ctx, a.CompanyID)
	if err != nil {
		return nil, err
	}
	branches, err := s.companies.ListBranches(ctx, a.CompanyID)
	if err != nil {
		return nil, err
	}
	state, err := s.repo.GetState(ctx, a.CompanyID)
	if err != nil {
		return nil, err
	}
	f, err := s.facts(ctx, a, branches)
	if err != nil {
		return nil, err
	}
	return &Overview{Company: *c, Branches: branches, State: *state, Facts: f, Progress: domain.Evaluate(*state, f)}, nil
}

// mutate applies change to the company state inside one transaction and
// records the audit event with the before/after snapshot.
func (s *Service) mutate(ctx context.Context, a Actor, action string, change func(ctx context.Context, st *domain.State, f domain.Facts) (map[string]any, error)) (*Overview, error) {
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Lock(ctx, a.CompanyID); err != nil {
			return err
		}
		st, err := s.repo.GetState(ctx, a.CompanyID)
		if err != nil {
			return err
		}
		branches, err := s.companies.ListBranches(ctx, a.CompanyID)
		if err != nil {
			return err
		}
		f, err := s.facts(ctx, a, branches)
		if err != nil {
			return err
		}
		before := domain.Evaluate(*st, f)
		extra, err := change(ctx, st, f)
		if err != nil {
			return err
		}
		st.CompanyID = a.CompanyID
		if err := s.repo.SaveState(ctx, st); err != nil {
			return err
		}
		after := domain.Evaluate(*st, f)
		companyID, branchID := a.CompanyID, a.BranchID
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: a.UserID, Action: action, EntityType: "company_setup",
			EntityID: &companyID, BranchID: &branchID,
			Before: progressSnapshot(before), After: progressSnapshot(after),
			Extra: extra, IP: a.IP, UserAgent: a.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.Overview(ctx, a)
}

func progressSnapshot(p domain.Progress) map[string]any {
	steps := make(map[string]string, len(p.Steps))
	for _, st := range p.Steps {
		steps[string(st.Key)] = string(st.Status)
	}
	return map[string]any{"steps": steps, "completed": p.Completed, "required": p.Required}
}

// SaveCompany validates and stores the full company profile (name, contact,
// location) and completes the company step. Without an explicit slug the URL
// follows the English name: renaming re-derives it, otherwise it is kept.
func (s *Service) SaveCompany(ctx context.Context, a Actor, c company.Company) (*Overview, error) {
	c.ID = a.CompanyID
	if strings.TrimSpace(c.Slug) == "" {
		current, err := s.companies.GetCompany(ctx, a.CompanyID)
		if err != nil {
			return nil, err
		}
		c.Slug = followNameSlug(current, c.NameEN)
	}
	if err := c.Normalize(company.ProfileComplete); err != nil {
		return nil, err
	}
	o, err := s.mutate(ctx, a, "setup.company_saved", func(ctx context.Context, st *domain.State, _ domain.Facts) (map[string]any, error) {
		if err := s.companies.UpdateCompany(ctx, &c, a.UserID); err != nil {
			return nil, err
		}
		if st.CompanyDoneAt == nil {
			now := s.now()
			st.CompanyDoneAt = &now
		}
		st.UpdatedAt = s.now()
		return map[string]any{"slug": c.Slug, "name_en": c.NameEN, "country": c.Country, "city": c.City,
			"currency": c.Currency, "timezone": c.Timezone}, nil
	})
	if err == nil {
		s.caches.Invalidate(a.CompanyID)
	}
	return o, err
}

func followNameSlug(current *company.Company, nameEN string) string {
	if strings.TrimSpace(nameEN) == current.NameEN {
		return current.Slug
	}
	if slug := company.Slugify(nameEN); slug != "" {
		return slug
	}
	return current.Slug
}

// Advance passes a step; skip is needed for AI or channels that are not
// configured yet.
func (s *Service) Advance(ctx context.Context, a Actor, step domain.StepKey, skip bool) (*Overview, error) {
	return s.mutate(ctx, a, "setup.step_advanced", func(_ context.Context, st *domain.State, f domain.Facts) (map[string]any, error) {
		if err := st.Advance(step, skip, f, s.now()); err != nil {
			return nil, err
		}
		return map[string]any{"step": step, "skip": skip}, nil
	})
}

func (s *Service) Complete(ctx context.Context, a Actor) (*Overview, error) {
	return s.mutate(ctx, a, "setup.completed", func(_ context.Context, st *domain.State, f domain.Facts) (map[string]any, error) {
		return nil, st.Complete(f, a.UserID, s.now())
	})
}

func (s *Service) Dismiss(ctx context.Context, a Actor) (*Overview, error) {
	return s.mutate(ctx, a, "setup.dismissed", func(_ context.Context, st *domain.State, _ domain.Facts) (map[string]any, error) {
		st.Dismiss(s.now())
		return nil, nil
	})
}
