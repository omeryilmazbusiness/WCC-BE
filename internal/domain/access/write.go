package access

import (
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ErrOwnerForbidden is returned when a caller assigns a record to someone
// else without the scope to do so.
var ErrOwnerForbidden = shared.NewForbidden("owner outside caller scope")

// ErrBranchRequired is returned when a write cannot infer its branch (a
// global caller without a home branch that did not name one).
var ErrBranchRequired = shared.NewValidation("branch_id is required")

// WriteBranch resolves the branch a new record is written to. Global scopes
// may target any branch (defaulting to their own); everyone else is pinned
// to their own branch.
func (s Scope) WriteBranch(requested uuid.UUID) (uuid.UUID, error) {
	var req *uuid.UUID
	if requested != uuid.Nil {
		req = &requested
	}
	b, err := s.ResolveBranch(req)
	if err != nil {
		return uuid.Nil, err
	}
	if b != nil && *b != uuid.Nil {
		return *b, nil
	}
	if s.BranchID != uuid.Nil {
		return s.BranchID, nil
	}
	return uuid.Nil, ErrBranchRequired
}

// ResolveOwner picks the owner of a record the caller creates or reassigns.
// A zero request defaults to the caller. Own-level callers may only own
// records themselves; team and wider scopes may assign anyone.
func (s Scope) ResolveOwner(requested uuid.UUID) (uuid.UUID, error) {
	if !s.Allowed() {
		return uuid.Nil, ErrNoScope
	}
	if requested == uuid.Nil {
		requested = s.UserID
	}
	if s.Level == LevelOwn && requested != s.UserID {
		return uuid.Nil, ErrOwnerForbidden
	}
	return requested, nil
}

// CanReassign reports whether the caller may change record ownership.
func (s Scope) CanReassign() bool { return s.IsElevated() }
