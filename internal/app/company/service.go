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
