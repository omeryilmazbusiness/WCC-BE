// Package access models data-visibility scope (PDF §3, §24): which branch,
// team and owner a caller may see. It is pure and fail-closed: the zero
// Scope grants nothing.
package access

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Level is the breadth of records a caller may access.
type Level int

const (
	LevelNone    Level = iota // zero value: deny everything
	LevelOwn                  // only records owned by the caller
	LevelTeam                 // records owned by members of the caller's team
	LevelBranch               // every record in the caller's branch
	LevelCompany              // every branch of the caller's company (GM); lists stay on the active branch
	LevelGlobal               // every branch of every company (platform admin, system jobs)
)

func (l Level) String() string {
	switch l {
	case LevelOwn:
		return "own"
	case LevelTeam:
		return "team"
	case LevelBranch:
		return "branch"
	case LevelCompany:
		return "company"
	case LevelGlobal:
		return "global"
	default:
		return "none"
	}
}

// ErrNoScope is returned when a data access runs without an access scope.
var ErrNoScope = errors.New("access scope missing")

// ErrBranchForbidden is returned when a caller requests a branch outside scope.
var ErrBranchForbidden = errors.New("branch outside caller scope")

// Scope is the caller's visibility envelope. BranchID is the branch the
// caller acts on; for company scopes it is the active branch and Branches
// lists every branch of CompanyID.
type Scope struct {
	Level     Level
	UserID    uuid.UUID
	BranchID  uuid.UUID
	TeamID    uuid.UUID
	CompanyID uuid.UUID
	Branches  []uuid.UUID
}

// Record is the minimal ownership footprint of a scoped entity.
type Record struct {
	BranchID    uuid.UUID
	OwnerID     *uuid.UUID
	OwnerTeamID *uuid.UUID
}

// System is the scope for trusted background work (workers, event reactors).
func System() Scope { return Scope{Level: LevelGlobal} }

// ForBranch is a branch-wide scope without a user (e.g. verified webhooks).
func ForBranch(branchID uuid.UUID) Scope {
	return Scope{Level: LevelBranch, BranchID: branchID}
}

func (s Scope) Allowed() bool { return s.Level > LevelNone }

// IsElevated reports whether the scope reaches beyond the caller's own records.
func (s Scope) IsElevated() bool { return s.Level >= LevelTeam }

// BranchFilter returns the single branch a query must be restricted to, if
// any. Company scopes span several branches: see CompanyBranches.
func (s Scope) BranchFilter() (uuid.UUID, bool) {
	if s.Level == LevelGlobal || s.Level == LevelCompany || s.Level == LevelNone {
		return uuid.Nil, false
	}
	return s.BranchID, true
}

// CompanyBranches returns the branch set a company scope is confined to.
func (s Scope) CompanyBranches() ([]uuid.UUID, bool) {
	if s.Level != LevelCompany {
		return nil, false
	}
	return s.Branches, true
}

func (s Scope) inCompany(branchID uuid.UUID) bool {
	for _, b := range s.Branches {
		if b == branchID {
			return true
		}
	}
	return false
}

// OwnerFilter returns the owner a query must be restricted to, if any.
func (s Scope) OwnerFilter() (uuid.UUID, bool) {
	if s.Level == LevelOwn {
		return s.UserID, true
	}
	return uuid.Nil, false
}

// TeamFilter returns the team whose members' records are visible, if any.
func (s Scope) TeamFilter() (uuid.UUID, bool) {
	if s.Level == LevelTeam {
		return s.TeamID, true
	}
	return uuid.Nil, false
}

// ResolveBranch turns an optional requested branch into the branch a query
// must use. Global scopes may pick any branch (nil = all); company scopes may
// pick a branch of their company and otherwise get the active branch, so a
// missing filter never widens past one tenant; everyone else is pinned to
// their own branch and may not ask for another.
func (s Scope) ResolveBranch(requested *uuid.UUID) (*uuid.UUID, error) {
	switch s.Level {
	case LevelNone:
		return nil, ErrNoScope
	case LevelGlobal:
		return requested, nil
	case LevelCompany:
		b := s.BranchID
		if requested != nil && *requested != uuid.Nil {
			b = *requested
		}
		if !s.inCompany(b) {
			return nil, ErrBranchForbidden
		}
		return &b, nil
	}
	if requested != nil && *requested != uuid.Nil && *requested != s.BranchID {
		return nil, ErrBranchForbidden
	}
	b := s.BranchID
	return &b, nil
}

// CanAccess decides whether a single record is visible.
func (s Scope) CanAccess(r Record) bool {
	switch s.Level {
	case LevelGlobal:
		return true
	case LevelCompany:
		return s.inCompany(r.BranchID)
	case LevelBranch:
		return r.BranchID == s.BranchID
	case LevelTeam:
		if r.BranchID != s.BranchID {
			return false
		}
		if r.OwnerID == nil {
			return true
		}
		if *r.OwnerID == s.UserID {
			return true
		}
		return r.OwnerTeamID != nil && *r.OwnerTeamID == s.TeamID
	case LevelOwn:
		return r.BranchID == s.BranchID && r.OwnerID != nil && *r.OwnerID == s.UserID
	default:
		return false
	}
}

// CanAccessCompany decides whether company-level rows (profile, branch list,
// onboarding) are visible: members see their own company, global scopes all.
func (s Scope) CanAccessCompany(companyID uuid.UUID) bool {
	if s.Level == LevelGlobal {
		return true
	}
	return s.Level > LevelNone && companyID != uuid.Nil && s.CompanyID == companyID
}

// CanAccessBranch decides branch-level visibility for unowned entities.
func (s Scope) CanAccessBranch(branchID uuid.UUID) bool {
	switch s.Level {
	case LevelGlobal:
		return true
	case LevelCompany:
		return s.inCompany(branchID)
	}
	return s.Level > LevelNone && branchID == s.BranchID
}

type ctxKey struct{}

// WithScope attaches a scope to ctx.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// From returns the scope on ctx; the zero (deny) Scope when absent.
func From(ctx context.Context) Scope {
	s, _ := ctx.Value(ctxKey{}).(Scope)
	return s
}

// Require returns the scope on ctx or ErrNoScope.
func Require(ctx context.Context) (Scope, error) {
	s := From(ctx)
	if !s.Allowed() {
		return s, ErrNoScope
	}
	return s, nil
}
