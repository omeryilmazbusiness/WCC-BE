package access

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestCanAccess(t *testing.T) {
	branch, other := uuid.New(), uuid.New()
	me, peer, stranger := uuid.New(), uuid.New(), uuid.New()
	team, otherTeam := uuid.New(), uuid.New()

	rec := func(b uuid.UUID, owner *uuid.UUID, ownerTeam *uuid.UUID) Record {
		return Record{BranchID: b, OwnerID: owner, OwnerTeamID: ownerTeam}
	}

	cases := []struct {
		name  string
		scope Scope
		rec   Record
		want  bool
	}{
		{"none denies", Scope{}, rec(branch, &me, nil), false},
		{"global any branch", System(), rec(other, &stranger, nil), true},
		{"branch same", Scope{Level: LevelBranch, BranchID: branch}, rec(branch, &stranger, nil), true},
		{"branch other", Scope{Level: LevelBranch, BranchID: branch}, rec(other, &stranger, nil), false},
		{"team own", Scope{Level: LevelTeam, BranchID: branch, UserID: me, TeamID: team}, rec(branch, &me, nil), true},
		{"team peer", Scope{Level: LevelTeam, BranchID: branch, UserID: me, TeamID: team}, rec(branch, &peer, &team), true},
		{"team outsider", Scope{Level: LevelTeam, BranchID: branch, UserID: me, TeamID: team}, rec(branch, &stranger, &otherTeam), false},
		{"team unassigned", Scope{Level: LevelTeam, BranchID: branch, UserID: me, TeamID: team}, rec(branch, nil, nil), true},
		{"team other branch", Scope{Level: LevelTeam, BranchID: branch, UserID: me, TeamID: team}, rec(other, &peer, &team), false},
		{"own mine", Scope{Level: LevelOwn, BranchID: branch, UserID: me}, rec(branch, &me, nil), true},
		{"own peer", Scope{Level: LevelOwn, BranchID: branch, UserID: me}, rec(branch, &peer, &team), false},
		{"own unassigned", Scope{Level: LevelOwn, BranchID: branch, UserID: me}, rec(branch, nil, nil), false},
		{"own other branch", Scope{Level: LevelOwn, BranchID: branch, UserID: me}, rec(other, &me, nil), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.scope.CanAccess(tc.rec); got != tc.want {
				t.Fatalf("CanAccess = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveBranch(t *testing.T) {
	branch, other := uuid.New(), uuid.New()

	if _, err := (Scope{}).ResolveBranch(nil); err != ErrNoScope {
		t.Fatalf("zero scope must fail closed, got %v", err)
	}
	got, err := System().ResolveBranch(&other)
	if err != nil || got == nil || *got != other {
		t.Fatalf("global may pick any branch: %v %v", got, err)
	}
	all, err := System().ResolveBranch(nil)
	if err != nil || all != nil {
		t.Fatalf("global nil means all branches: %v %v", all, err)
	}
	s := Scope{Level: LevelOwn, BranchID: branch}
	pinned, err := s.ResolveBranch(nil)
	if err != nil || *pinned != branch {
		t.Fatalf("non-global pinned to own branch: %v %v", pinned, err)
	}
	if _, err := s.ResolveBranch(&other); err != ErrBranchForbidden {
		t.Fatalf("non-global cannot request other branch, got %v", err)
	}
}

func TestFilters(t *testing.T) {
	b, u, tm := uuid.New(), uuid.New(), uuid.New()
	own := Scope{Level: LevelOwn, BranchID: b, UserID: u}
	if id, ok := own.OwnerFilter(); !ok || id != u {
		t.Fatal("own scope filters by owner")
	}
	if id, ok := own.BranchFilter(); !ok || id != b {
		t.Fatal("own scope filters by branch")
	}
	team := Scope{Level: LevelTeam, BranchID: b, UserID: u, TeamID: tm}
	if id, ok := team.TeamFilter(); !ok || id != tm {
		t.Fatal("team scope filters by team")
	}
	if _, ok := System().BranchFilter(); ok {
		t.Fatal("global scope has no branch filter")
	}
}

func TestContext(t *testing.T) {
	if _, err := Require(context.Background()); err != ErrNoScope {
		t.Fatal("missing scope must fail closed")
	}
	ctx := WithScope(context.Background(), System())
	if s, err := Require(ctx); err != nil || s.Level != LevelGlobal {
		t.Fatalf("scope round-trip failed: %v", err)
	}
}
