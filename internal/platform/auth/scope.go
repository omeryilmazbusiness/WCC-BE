package auth

import (
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

// ScopeLevelFor maps a role to its data-visibility breadth (PDF §3):
// GMs see every branch of their company, the platform Admin every company,
// Finance/Operations the whole branch, Managers their team (branch when
// unassigned to a team), Employees their own records.
func ScopeLevelFor(role Role, teamID uuid.UUID) access.Level {
	switch role {
	case RoleGM:
		return access.LevelCompany
	case RoleAdmin:
		return access.LevelGlobal
	case RoleFinance, RoleOperations:
		return access.LevelBranch
	case RoleManager:
		if teamID == uuid.Nil {
			return access.LevelBranch
		}
		return access.LevelTeam
	case RoleEmployee:
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
