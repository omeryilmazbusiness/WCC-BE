package pgscope

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

var cols = Columns{Branch: "l.branch_id", Owner: "l.owner_id"}

func TestFailsClosedWithoutScope(t *testing.T) {
	if _, _, err := Clause(context.Background(), cols, nil); err == nil {
		t.Fatal("expected error without scope")
	}
}

func TestGlobalAddsNothing(t *testing.T) {
	ctx := access.WithScope(context.Background(), access.System())
	sql, args, err := Clause(ctx, cols, []any{"x"})
	if err != nil || sql != "" || len(args) != 1 {
		t.Fatalf("global must not filter: %q %v %v", sql, args, err)
	}
}

func TestOwnScope(t *testing.T) {
	b, u := uuid.New(), uuid.New()
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelOwn, BranchID: b, UserID: u})
	sql, args, err := Clause(ctx, cols, []any{"id"})
	if err != nil {
		t.Fatal(err)
	}
	if sql != " AND l.branch_id=$2 AND l.owner_id=$3" {
		t.Fatalf("unexpected sql %q", sql)
	}
	if args[1] != b || args[2] != u {
		t.Fatalf("unexpected args %v", args)
	}
}

func TestTeamScopeWithUnassigned(t *testing.T) {
	b, u, tm := uuid.New(), uuid.New(), uuid.New()
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelTeam, BranchID: b, UserID: u, TeamID: tm})
	c := cols
	c.OwnerOrUnassigned = true
	sql, args, err := Clause(ctx, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "l.owner_id IS NULL OR") || !strings.Contains(sql, "team_id=$3") || len(args) != 3 {
		t.Fatalf("unexpected team sql %q %v", sql, args)
	}
}

func TestBranchOnlyEntity(t *testing.T) {
	b, u := uuid.New(), uuid.New()
	ctx := access.WithScope(context.Background(), access.Scope{Level: access.LevelOwn, BranchID: b, UserID: u})
	sql, _, err := Clause(ctx, Columns{Branch: "c.branch_id"}, nil)
	if err != nil || sql != " AND c.branch_id=$1" {
		t.Fatalf("branch-only entity must ignore owner: %q %v", sql, err)
	}
}
