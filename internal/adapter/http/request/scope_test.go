package request

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func reqWithScope(t *testing.T, target string, s access.Scope) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, target, nil)
	return r.WithContext(access.WithScope(r.Context(), s))
}

func TestBranch(t *testing.T) {
	own, other := uuid.New(), uuid.New()
	global := access.Scope{Level: access.LevelGlobal, BranchID: own}
	branch := access.Scope{Level: access.LevelBranch, BranchID: own}

	got, err := Branch(reqWithScope(t, "/x", global))
	if err != nil || got != nil {
		t.Fatalf("global without filter: got %v, %v; want nil (all)", got, err)
	}
	got, err = Branch(reqWithScope(t, "/x?branch_id="+other.String(), global))
	if err != nil || got == nil || *got != other {
		t.Fatalf("global picks branch: got %v, %v", got, err)
	}
	got, err = Branch(reqWithScope(t, "/x", branch))
	if err != nil || got == nil || *got != own {
		t.Fatalf("branch scope pinned: got %v, %v", got, err)
	}
	if _, err = Branch(reqWithScope(t, "/x?branch_id="+other.String(), branch)); !errors.Is(err, access.ErrBranchForbidden) {
		t.Fatalf("branch scope other branch: got %v", err)
	}
	if _, err = Branch(httptest.NewRequest(http.MethodGet, "/x", nil)); !errors.Is(err, access.ErrNoScope) {
		t.Fatalf("missing scope: got %v", err)
	}
	if _, err = Branch(reqWithScope(t, "/x?branch_id=nope", global)); err == nil {
		t.Fatal("invalid branch_id accepted")
	}
}

func TestTargetBranch(t *testing.T) {
	own, other := uuid.New(), uuid.New()
	global := access.Scope{Level: access.LevelGlobal, BranchID: own}
	emp := access.Scope{Level: access.LevelOwn, BranchID: own, UserID: uuid.New()}

	if got, err := TargetBranch(reqWithScope(t, "/x", global)); err != nil || got != own {
		t.Fatalf("global default: got %v, %v", got, err)
	}
	if got, err := TargetBranch(reqWithScope(t, "/x?branch_id="+other.String(), global)); err != nil || got != other {
		t.Fatalf("global override: got %v, %v", got, err)
	}
	if got, err := TargetBranch(reqWithScope(t, "/x", emp)); err != nil || got != own {
		t.Fatalf("employee default: got %v, %v", got, err)
	}
	if _, err := TargetBranch(reqWithScope(t, "/x?branch_id="+other.String(), emp)); !errors.Is(err, access.ErrBranchForbidden) {
		t.Fatalf("employee other branch: got %v", err)
	}
	if _, err := TargetBranch(reqWithScope(t, "/x", access.System())); !errors.Is(err, access.ErrBranchForbidden) {
		t.Fatalf("system without branch: got %v", err)
	}
	if _, err := TargetBranch(httptest.NewRequest(http.MethodGet, "/x", nil)); !errors.Is(err, access.ErrNoScope) {
		t.Fatalf("missing scope: got %v", err)
	}
}
