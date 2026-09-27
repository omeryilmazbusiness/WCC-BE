package request

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

// Branch resolves the optional ?branch_id= filter against the caller's scope:
// global callers may pick any branch (nil = all branches), everyone else is
// pinned to their own branch and gets access.ErrBranchForbidden for another.
func Branch(r *http.Request) (*uuid.UUID, error) {
	requested, err := OptionalUUID(r, "branch_id")
	if err != nil {
		return nil, err
	}
	s, err := access.Require(r.Context())
	if err != nil {
		return nil, err
	}
	return s.ResolveBranch(requested)
}

// TargetBranch resolves the single branch a request acts on (creates,
// per-branch settings): the requested ?branch_id= when the scope allows it,
// otherwise the caller's own branch.
func TargetBranch(r *http.Request) (uuid.UUID, error) {
	requested, err := OptionalUUID(r, "branch_id")
	if err != nil {
		return uuid.Nil, err
	}
	return targetBranch(access.From(r.Context()), requested)
}

func targetBranch(s access.Scope, requested *uuid.UUID) (uuid.UUID, error) {
	b, err := s.ResolveBranch(requested)
	if err != nil {
		return uuid.Nil, err
	}
	if b != nil && *b != uuid.Nil {
		return *b, nil
	}
	if s.BranchID == uuid.Nil {
		return uuid.Nil, access.ErrBranchForbidden
	}
	return s.BranchID, nil
}
