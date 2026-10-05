package task

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

// PackageLinker checks a package link against the catalogue (DIP). It returns
// the link with a lone departure's package filled in, or a validation error
// when the package or departure is outside branchID.
type PackageLinker interface {
	ResolvePackageLink(ctx context.Context, branchID uuid.UUID, link domain.PackageLink) (domain.PackageLink, error)
}

func (s *Service) SetPackageLinker(l PackageLinker) { s.packages = l }

// LinkPackageInput replaces a task's package link; both nil clears it.
type LinkPackageInput struct {
	PackageID   *uuid.UUID
	DepartureID *uuid.UUID
}

func (s *Service) resolveLink(ctx context.Context, branchID uuid.UUID, link domain.PackageLink) (domain.PackageLink, error) {
	if link.Empty() {
		return link, nil
	}
	if s.packages == nil {
		return link, shared.NewValidation("package links are not available")
	}
	return s.packages.ResolvePackageLink(ctx, branchID, link)
}

// LinkPackage ties an open task to a package (and departure) of its branch,
// or clears the link, and returns the task with the link enriched.
func (s *Service) LinkPackage(ctx context.Context, id uuid.UUID, in LinkPackageInput) (*domain.Task, error) {
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("task")
		}
		link, err := s.resolveLink(ctx, t.BranchID, domain.NormalizeLink(in.PackageID, in.DepartureID))
		if err != nil {
			return err
		}
		if link.SameAs(t.Package) {
			return nil
		}
		if err := t.LinkPackage(link, s.now()); err != nil {
			return err
		}
		return s.repo.Update(ctx, t)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}
