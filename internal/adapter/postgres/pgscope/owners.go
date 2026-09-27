package pgscope

import (
	"context"
	"fmt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Owners renders the set of user ids whose records the scope may see, for
// ownership checks that go through a parent row (`owner_id IN (<set>)`).
// restricted is false for branch/global scopes, which need no owner check.
func Owners(ctx context.Context, args []any) (set string, out []any, restricted bool, err error) {
	s, err := access.Require(ctx)
	if err != nil {
		return "", args, false, shared.NewForbidden("access scope missing")
	}
	if u, ok := s.OwnerFilter(); ok {
		args = append(args, u)
		return fmt.Sprintf("$%d", len(args)), args, true, nil
	}
	if t, ok := s.TeamFilter(); ok {
		args = append(args, s.UserID, t)
		return fmt.Sprintf("SELECT $%d::uuid UNION SELECT id FROM users WHERE team_id=$%d", len(args)-1, len(args)), args, true, nil
	}
	return "", args, false, nil
}
