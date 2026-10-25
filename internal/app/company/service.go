// Package company is the application layer for tenants: platform admins
// register companies (with their main center and GM), GMs manage branches.
package company

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// GMAccount is the first user of a new company.
type GMAccount struct {
	FullName string
	Email    string
	Password string
}

// UserCreator creates the GM through the identity module, which owns
// password policy, hashing and the user audit trail.
type UserCreator interface {
	CreateGM(ctx context.Context, branchID uuid.UUID, gm GMAccount, actor Actor) (uuid.UUID, error)
}

// Invalidator drops cached tenant lookups after branch changes.
type Invalidator interface {
	Invalidate(companyID uuid.UUID)
}

type Actor struct {
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

type Service struct {
	repo   domain.Repository
	users  UserCreator
	tx     tx.Runner
	audit  audit.Recorder
	caches Invalidator
}

func NewService(repo domain.Repository, users UserCreator, runner tx.Runner, rec audit.Recorder, caches Invalidator) *Service {
	return &Service{repo: repo, users: users, tx: runner, audit: rec, caches: caches}
}

type RegisterInput struct {
	Company domain.Company
	GM      GMAccount
	Actor   Actor
}

type Registered struct {
	Company    domain.Company
	MainCenter domain.Branch
	GMUserID   uuid.UUID
}

const codeAttempts = 5

// Register creates a company with its default main center and GM account in
// one unit of work.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*Registered, error) {
	c := in.Company
	c.ID = uuid.New()
	fields := map[string]any{}
	if err := c.Normalize(domain.ProfileBasic); err != nil {
		var app *shared.AppError
		if !errors.As(err, &app) {
			return nil, err
		}
		for k, v := range app.Details {
			fields[k] = v
		}
	}
	gm := GMAccount{
		FullName: strings.TrimSpace(in.GM.FullName),
		Email:    strings.ToLower(strings.TrimSpace(in.GM.Email)),
		Password: in.GM.Password,
	}
	if len([]rune(gm.FullName)) < 2 {
		fields["gm_full_name"] = "required"
	}
	if !strings.Contains(gm.Email, "@") {
		fields["gm_email"] = "invalid email"
	}
	if err := platformauth.ValidatePassword(gm.Password); err != nil {
		fields["gm_password"] = err.Error()
	}
	if len(fields) > 0 {
		err := shared.NewValidation("invalid company registration")
		err.Details = fields
		return nil, err
	}

	out := &Registered{Company: c}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateCompany(ctx, &out.Company); err != nil {
			return err
		}
		main := domain.NewMainCenter(&out.Company)
		if err := s.createWithFreeCode(ctx, &main, true); err != nil {
			return err
		}
		out.MainCenter = main
		id, err := s.users.CreateGM(ctx, main.ID, gm, in.Actor)
		if err != nil {
			return err
		}
		out.GMUserID = id
		companyID, branchID := out.Company.ID, main.ID
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: in.Actor.UserID, Action: "company.registered", EntityType: "company",
			EntityID: &companyID, BranchID: &branchID,
			After: map[string]any{
				"slug": out.Company.Slug, "name_en": out.Company.NameEN,
				"main_center": main.Slug, "gm_user_id": id,
			},
			IP: in.Actor.IP, UserAgent: in.Actor.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// createWithFreeCode inserts a branch; an auto-generated code that collides
// with another company's gets a numeric suffix.
func (s *Service) createWithFreeCode(ctx context.Context, b *domain.Branch, autoCode bool) error {
	base := b.Code
	for i := 0; ; i++ {
		err := s.repo.CreateBranch(ctx, b)
		if err == nil || !autoCode || !isFieldConflict(err, "code") || i >= codeAttempts {
			return err
		}
		suffix := fmt.Sprintf("%d", i+2)
		if len(base)+len(suffix) > 16 {
			base = base[:16-len(suffix)]
		}
		b.Code = base + suffix
	}
}

func isFieldConflict(err error, field string) bool {
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(err, shared.ErrConflict) {
		return false
	}
	_, ok := app.Details[field]
	return ok
}

func (s *Service) ListCompanies(ctx context.Context, query string, limit, offset int) (domain.Page, error) {
	return s.repo.ListCompanies(ctx, query, limit, offset)
}

func requirePlatform(ctx context.Context) error {
	if access.From(ctx).Level != access.LevelGlobal {
		return shared.NewForbidden("platform scope required")
	}
	return nil
}

// GetCompany loads one tenant. The repository already hides companies
// outside the caller's scope (platform sees all, a desk sees only its own).
func (s *Service) GetCompany(ctx context.Context, id uuid.UUID) (*domain.Company, error) {
	return s.repo.GetCompany(ctx, id)
}

// SaveCompany writes the profile from the platform console. Plan changes
// go through UpdatePlan so a rename never resets the package year.
func (s *Service) SaveCompany(ctx context.Context, id uuid.UUID, in domain.Company, actor Actor) (*domain.Company, error) {
	if err := requirePlatform(ctx); err != nil {
		return nil, err
	}
	current, err := s.repo.GetCompany(ctx, id)
	if err != nil {
		return nil, err
	}
	in.ID = id
	in.Plan = current.Plan
	in.PlanStartedAt = current.PlanStartedAt
	in.IdleLogoutMinutes = current.IdleLogoutMinutes
	in.IsActive = current.IsActive
	if in.LegalName == "" {
		in.LegalName = current.LegalName
	}
	if in.Phone == "" {
		in.Phone = current.Phone
	}
	if in.Email == "" {
		in.Email = current.Email
	}
	if in.Website == "" {
		in.Website = current.Website
	}
	if in.Currency == "" {
		in.Currency = current.Currency
	}
	if in.Timezone == "" {
		in.Timezone = current.Timezone
	}
	if in.Slug == "" {
		in.Slug = current.Slug
	}
	if err := in.Normalize(domain.ProfileBasic); err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdateCompany(ctx, &in, actor.UserID); err != nil {
			return err
		}
		s.caches.Invalidate(id)
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: actor.UserID, Action: "company.updated", EntityType: "company",
			EntityID: &id, After: map[string]any{"slug": in.Slug, "name_en": in.NameEN},
			IP: actor.IP, UserAgent: actor.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.GetCompany(ctx, id)
}

// UpdatePlan changes the commercial package of a tenant (platform only)
// and opens a new 1-year term.
func (s *Service) UpdatePlan(ctx context.Context, companyID uuid.UUID, plan domain.Plan, actor Actor) (*domain.Company, error) {
	if err := requirePlatform(ctx); err != nil {
		return nil, err
	}
	if !domain.ValidPlan(plan) {
		err := shared.NewValidation("invalid plan")
		err.Details = map[string]any{"plan": "standard, premium or ultimate"}
		return nil, err
	}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.UpdatePlan(ctx, companyID, plan, actor.UserID); err != nil {
			return err
		}
		s.caches.Invalidate(companyID)
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: actor.UserID, Action: "company.plan_changed", EntityType: "company",
			EntityID: &companyID,
			After:    map[string]any{"plan": plan},
			IP:       actor.IP, UserAgent: actor.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	return s.repo.GetCompany(ctx, companyID)
}

func companyOf(ctx context.Context) (uuid.UUID, error) {
	sc, err := access.Require(ctx)
	if err != nil {
		return uuid.Nil, shared.NewForbidden("access scope missing")
	}
	if sc.CompanyID == uuid.Nil {
		return uuid.Nil, shared.NewForbidden("no company in scope")
	}
	return sc.CompanyID, nil
}

// Branches lists the caller's company branches, main center first.
func (s *Service) Branches(ctx context.Context) ([]domain.Branch, error) {
	id, err := companyOf(ctx)
	if err != nil {
		return nil, err
	}
	return s.repo.ListBranches(ctx, id)
}

type BranchInput struct {
	NameEN   string
	NameAR   string
	Slug     string
	Code     string
	Kind     domain.Kind
	Timezone string
	Actor    Actor
}

// CreateBranch adds a branch to the caller's company. Making it the main
// center demotes the previous one.
func (s *Service) CreateBranch(ctx context.Context, in BranchInput) (*domain.Branch, error) {
	companyID, err := companyOf(ctx)
	if err != nil {
		return nil, err
	}
	b := domain.Branch{
		ID: uuid.New(), CompanyID: companyID, NameEN: in.NameEN, NameAR: in.NameAR,
		Slug: in.Slug, Code: strings.TrimSpace(in.Code), Kind: in.Kind, Timezone: in.Timezone, IsActive: true,
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LockCompany(ctx, companyID); err != nil {
			return err
		}
		c, err := s.repo.GetCompany(ctx, companyID)
		if err != nil {
			return err
		}
		used, err := s.repo.CountUsage(ctx, companyID, domain.LimitBranches)
		if err != nil {
			return err
		}
		if err := domain.Refuse(c.Plan, domain.LimitBranches, used); err != nil {
			return err
		}
		if b.Timezone == "" {
			b.Timezone = c.Timezone
		}
		autoCode := b.Code == ""
		if autoCode {
			slug := b.Slug
			if slug == "" {
				slug = domain.Slugify(b.NameEN)
			}
			b.Code = domain.BranchCode(c.Slug, slug)
		}
		if err := b.Normalize(); err != nil {
			return err
		}
		if b.Kind == domain.KindMainCenter {
			if err := s.repo.DemoteMainCenter(ctx, companyID, b.ID); err != nil {
				return err
			}
		}
		if err := s.createWithFreeCode(ctx, &b, autoCode); err != nil {
			return err
		}
		id := b.ID
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: in.Actor.UserID, Action: "branch.created", EntityType: "branch",
			EntityID: &id, BranchID: &id,
			After: branchSnapshot(&b), IP: in.Actor.IP, UserAgent: in.Actor.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	s.caches.Invalidate(companyID)
	return &b, nil
}

type BranchPatch struct {
	NameEN   *string
	NameAR   *string
	Slug     *string
	Code     *string
	Kind     *domain.Kind
	Timezone *string
	Actor    Actor
}

// UpdateBranch edits a branch of the caller's company. Renaming re-derives
// the URL slug unless one is given.
func (s *Service) UpdateBranch(ctx context.Context, id uuid.UUID, p BranchPatch) (*domain.Branch, error) {
	companyID, err := companyOf(ctx)
	if err != nil {
		return nil, err
	}
	var out domain.Branch
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LockCompany(ctx, companyID); err != nil {
			return err
		}
		branches, err := s.repo.ListBranches(ctx, companyID)
		if err != nil {
			return err
		}
		ws := domain.Workspace{Branches: branches}
		cur, ok := ws.Branch(id)
		if !ok {
			return shared.NewNotFound("branch")
		}
		before := branchSnapshot(&cur)
		next := cur
		if p.NameEN != nil && strings.TrimSpace(*p.NameEN) != cur.NameEN {
			next.NameEN = *p.NameEN
			if p.Slug == nil {
				next.Slug = ""
			}
		}
		if p.NameAR != nil {
			next.NameAR = *p.NameAR
		}
		if p.Slug != nil {
			next.Slug = *p.Slug
		}
		if p.Code != nil {
			next.Code = *p.Code
		}
		if p.Kind != nil {
			next.Kind = *p.Kind
		}
		if p.Timezone != nil {
			next.Timezone = *p.Timezone
		}
		if err := next.Normalize(); err != nil {
			return err
		}
		if cur.Kind == domain.KindMainCenter && next.Kind != domain.KindMainCenter {
			err := shared.NewValidation("a company always keeps one main center")
			err.Details = map[string]any{"kind": "promote another branch to main center instead"}
			return err
		}
		if next.Kind == domain.KindMainCenter && cur.Kind != domain.KindMainCenter {
			if err := s.repo.DemoteMainCenter(ctx, companyID, next.ID); err != nil {
				return err
			}
		}
		if err := s.repo.UpdateBranch(ctx, &next); err != nil {
			return err
		}
		out = next
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: p.Actor.UserID, Action: "branch.updated", EntityType: "branch",
			EntityID: &id, BranchID: &id,
			Before: before, After: branchSnapshot(&next), IP: p.Actor.IP, UserAgent: p.Actor.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	s.caches.Invalidate(companyID)
	return &out, nil
}

func branchSnapshot(b *domain.Branch) map[string]any {
	return map[string]any{
		"code": b.Code, "slug": b.Slug, "name_en": b.NameEN, "name_ar": b.NameAR,
		"kind": b.Kind, "timezone": b.Timezone,
	}
}
