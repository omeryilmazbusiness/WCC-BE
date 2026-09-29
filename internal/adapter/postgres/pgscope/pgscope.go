// Package pgscope translates an access.Scope on ctx into SQL predicates so
// every repository enforces branch / team / owner visibility the same way.
package pgscope

import (
	"context"
	"fmt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Columns names the scoping columns of a table (qualified with alias when
// the query joins). Owner is empty for branch-wide entities.
type Columns struct {
	Branch string
	Owner  string
	// OwnerOrUnassigned lets own/team scopes also see rows with NULL owner
	// (e.g. the Unassigned inbox queue).
	OwnerOrUnassigned bool
}

// Append adds scope predicates to an existing where/args builder. Placeholders
// continue from len(args)+1. It fails closed when ctx carries no scope.
func Append(ctx context.Context, cols Columns, where []string, args []any) ([]string, []any, error) {
	s, err := access.Require(ctx)
	if err != nil {
		return where, args, shared.NewForbidden("access scope missing")
	}
	if b, ok := s.BranchFilter(); ok && cols.Branch != "" {
		args = append(args, b)
		where = append(where, fmt.Sprintf("%s=$%d", cols.Branch, len(args)))
	}
	if set, ok := s.CompanyBranches(); ok && cols.Branch != "" {
		args = append(args, set)
		where = append(where, fmt.Sprintf("%s = ANY($%d::uuid[])", cols.Branch, len(args)))
	}
	if cols.Owner == "" {
		return where, args, nil
	}
	if u, ok := s.OwnerFilter(); ok {
		args = append(args, u)
		pred := fmt.Sprintf("%s=$%d", cols.Owner, len(args))
		where = append(where, orUnassigned(cols, pred))
		return where, args, nil
	}
	if t, ok := s.TeamFilter(); ok {
		args = append(args, s.UserID, t)
		pred := fmt.Sprintf("(%s=$%d OR %s IN (SELECT id FROM users WHERE team_id=$%d))",
			cols.Owner, len(args)-1, cols.Owner, len(args))
		where = append(where, orUnassigned(cols, pred))
	}
	return where, args, nil
}

// Clause renders Append as a " AND ..." suffix for queries that already have
// a WHERE with len(args) placeholders bound.
func Clause(ctx context.Context, cols Columns, args []any) (string, []any, error) {
	where, args, err := Append(ctx, cols, nil, args)
	if err != nil {
		return "", args, err
	}
	out := ""
	for _, w := range where {
		out += " AND " + w
	}
	return out, args, nil
}

func orUnassigned(cols Columns, pred string) string {
	if cols.OwnerOrUnassigned {
		return fmt.Sprintf("(%s IS NULL OR %s)", cols.Owner, pred)
	}
	return pred
}
