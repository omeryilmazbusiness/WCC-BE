package pgscope

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// EnsureBranch guards writes of branch-owned rows: the target branch must be
// inside the caller's scope. It fails closed when ctx carries no scope.
func EnsureBranch(ctx context.Context, branchID uuid.UUID) error {
	s, err := access.Require(ctx)
	if err != nil {
		return shared.NewForbidden("access scope missing")
	}
	if !s.CanAccessBranch(branchID) {
		return access.ErrBranchForbidden
	}
	return nil
}
