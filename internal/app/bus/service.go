// Package bus runs the operations coach-operator catalogue.
package bus

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/bus"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// CompanyInput is the editable operator card.
type CompanyInput struct {
	Name         string
	Phone        string
	Email        string
	Website      string
	Country      string
	City         string
	Address      string
	VehicleClass string
	LicenseNo    string
	ContactName  string
	ContactPhone string
	Notes        string
	IsActive     *bool
}

type Service struct {
	repo  domain.Repository
	tx    tx.Runner
	audit audit.Recorder
	now   func() time.Time
}

func NewService(repo domain.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, tx: txm, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

func (in CompanyInput) apply(c *domain.Company) {
	c.Name, c.Phone, c.Email, c.Website = in.Name, in.Phone, in.Email, in.Website
	c.Country, c.City, c.Address = in.Country, in.City, in.Address
	c.VehicleClass, c.LicenseNo = in.VehicleClass, in.LicenseNo
	c.ContactName, c.ContactPhone, c.Notes = in.ContactName, in.ContactPhone, in.Notes
	if in.IsActive != nil {
		c.IsActive = *in.IsActive
	}
}

func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.Company, error) {
	if f.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	return s.repo.List(ctx, f)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Company, error) {
	return s.repo.Find(ctx, id)
}

func (s *Service) Create(ctx context.Context, branchID uuid.UUID, in CompanyInput) (*domain.Company, error) {
	if branchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	now := s.now()
	c := &domain.Company{ID: uuid.New(), BranchID: branchID, IsActive: true, CreatedAt: now, UpdatedAt: now}
	in.apply(c)
	if err := c.Normalize(); err != nil {
		return nil, err
	}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, c); err != nil {
			return err
		}
		return s.record(ctx, "bus.created", c, nil, snapshot(c), nil)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in CompanyInput) (*domain.Company, error) {
	var out *domain.Company
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		c, err := s.repo.Find(ctx, id)
		if err != nil {
			return err
		}
		before := snapshot(c)
		in.apply(c)
		c.UpdatedAt = s.now()
		if err := c.Normalize(); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, c); err != nil {
			return err
		}
		out = c
		return s.record(ctx, "bus.updated", c, before, snapshot(c), nil)
	})
	return out, err
}

func (s *Service) record(ctx context.Context, action string, c *domain.Company, before, after, extra map[string]any) error {
	if s.audit == nil {
		return nil
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra["name"] = c.Name
	id, branch := c.ID, c.BranchID
	return s.audit.Record(ctx, audit.RecordInput{
		Action: action, EntityType: "bus_company", EntityID: &id, BranchID: &branch,
		Before: before, After: after, Extra: extra,
	})
}

func snapshot(c *domain.Company) map[string]any {
	return map[string]any{
		"name": c.Name, "phone": c.Phone, "email": c.Email, "city": c.City, "country": c.Country,
		"vehicle_class": c.VehicleClass, "is_active": c.IsActive,
	}
}
