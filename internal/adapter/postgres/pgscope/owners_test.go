package pgscope

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func TestOwners(t *testing.T) {
	me, team, branch := uuid.New(), uuid.New(), uuid.New()
	ctxFor := func(s access.Scope) context.Context { return access.WithScope(context.Background(), s) }

	set, args, restricted, err := Owners(ctxFor(access.Scope{Level: access.LevelOwn, UserID: me, BranchID: branch}), []any{"x"})
	if err != nil || !restricted || set != "$2" || len(args) != 2 || args[1] != me {
		t.Fatalf("own: %q %v %v %v", set, args, restricted, err)
	}

	set, args, restricted, err = Owners(ctxFor(access.Scope{Level: access.LevelTeam, UserID: me, TeamID: team, BranchID: branch}), nil)
	want := "SELECT $1::uuid UNION SELECT id FROM users WHERE team_id=$2"
	if err != nil || !restricted || set != want || len(args) != 2 || args[0] != me || args[1] != team {
		t.Fatalf("team: %q %v %v %v", set, args, restricted, err)
	}

	for _, s := range []access.Scope{{Level: access.LevelBranch, BranchID: branch}, access.System()} {
		set, args, restricted, err = Owners(ctxFor(s), nil)
		if err != nil || restricted || set != "" || len(args) != 0 {
			t.Fatalf("%v: %q %v %v %v", s.Level, set, args, restricted, err)
		}
	}

	if _, _, _, err = Owners(context.Background(), nil); err == nil {
		t.Fatal("missing scope allowed")
	}
}
