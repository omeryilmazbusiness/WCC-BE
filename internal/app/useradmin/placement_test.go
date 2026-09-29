package useradmin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

func TestPlacementPlatformAdminHasNoBranch(t *testing.T) {
	branch := uuid.New()
	platform := access.WithScope(context.Background(), access.Scope{Level: access.LevelGlobal})
	company := access.WithScope(context.Background(), access.Scope{
		Level: access.LevelCompany, BranchID: branch, CompanyID: uuid.New(), Branches: []uuid.UUID{branch},
	})

	if err := checkPlacement(platform, platformauth.RoleAdmin, uuid.Nil); err != nil {
		t.Fatalf("platform admin without branch: %v", err)
	}
	if err := checkPlacement(platform, platformauth.RoleAdmin, branch); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("admin placed in a branch must be rejected, got %v", err)
	}
	if err := checkPlacement(company, platformauth.RoleAdmin, uuid.Nil); !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("GM granting admin must be forbidden, got %v", err)
	}
	if err := checkPlacement(platform, platformauth.RoleGM, uuid.Nil); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("company roles need a branch, got %v", err)
	}
	if err := checkPlacement(company, platformauth.RoleEmployee, branch); err != nil {
		t.Fatalf("employee in own company: %v", err)
	}
	if err := checkPlacement(company, platformauth.RoleEmployee, uuid.New()); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("foreign branch must be rejected, got %v", err)
	}
}
