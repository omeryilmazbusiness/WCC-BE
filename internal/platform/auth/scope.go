package auth

import (
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

// ScopeLevelFor maps a role to its data-visibility breadth:
// GMs see every branch of their company, the platform Admin every company,
// Finance the whole branch, Agents their own records.
func ScopeLevelFor(role Role, teamID uuid.UUID) access.Level {
	switch CanonicalRole(role) {
	case RoleGM:
		return access.LevelCompany
	case RoleAdmin:
		return access.LevelGlobal
	case RoleFinance:
		return access.LevelBranch
	case RoleAgent:
		return access.LevelOwn
	default:
		return access.LevelNone
	}
}

// ScopeFor derives the access scope carried by an authenticated request.
func ScopeFor(c *Claims) access.Scope {
	if c == nil {
		return access.Scope{}
	}
	return access.Scope{
		Level:    ScopeLevelFor(c.Role, c.TeamID),
		UserID:   c.UserID,
		BranchID: c.BranchID,
		TeamID:   c.TeamID,
	}
}
