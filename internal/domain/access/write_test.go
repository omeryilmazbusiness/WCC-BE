package access

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestWriteBranch(t *testing.T) {
	home, other := uuid.New(), uuid.New()
	cases := []struct {
		name    string
		scope   Scope
		req     uuid.UUID
		want    uuid.UUID
		wantErr error
	}{
		{"own defaults home", Scope{Level: LevelOwn, BranchID: home}, uuid.Nil, home, nil},
		{"branch same", Scope{Level: LevelBranch, BranchID: home}, home, home, nil},
		{"branch other denied", Scope{Level: LevelBranch, BranchID: home}, other, uuid.Nil, ErrBranchForbidden},
		{"team other denied", Scope{Level: LevelTeam, BranchID: home}, other, uuid.Nil, ErrBranchForbidden},
		{"global picks other", Scope{Level: LevelGlobal, BranchID: home}, other, other, nil},
		{"global defaults home", Scope{Level: LevelGlobal, BranchID: home}, uuid.Nil, home, nil},
		{"system needs branch", System(), uuid.Nil, uuid.Nil, ErrBranchRequired},
		{"system explicit", System(), other, other, nil},
		{"none denied", Scope{}, home, uuid.Nil, ErrNoScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.scope.WriteBranch(tc.req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("branch = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveOwner(t *testing.T) {
	me, other := uuid.New(), uuid.New()
	cases := []struct {
		name    string
		scope   Scope
		req     uuid.UUID
		want    uuid.UUID
		wantErr error
	}{
		{"own defaults self", Scope{Level: LevelOwn, UserID: me}, uuid.Nil, me, nil},
		{"own self ok", Scope{Level: LevelOwn, UserID: me}, me, me, nil},
		{"own other denied", Scope{Level: LevelOwn, UserID: me}, other, uuid.Nil, ErrOwnerForbidden},
		{"team assigns other", Scope{Level: LevelTeam, UserID: me}, other, other, nil},
		{"branch assigns other", Scope{Level: LevelBranch, UserID: me}, other, other, nil},
		{"global assigns other", Scope{Level: LevelGlobal, UserID: me}, other, other, nil},
		{"none denied", Scope{}, me, uuid.Nil, ErrNoScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.scope.ResolveOwner(tc.req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("owner = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanReassign(t *testing.T) {
	if (Scope{Level: LevelOwn}).CanReassign() {
		t.Fatal("own scope must not reassign")
	}
	if !(Scope{Level: LevelTeam}).CanReassign() {
		t.Fatal("team scope must reassign")
	}
}
