package access

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func companyScope() (Scope, uuid.UUID, uuid.UUID, uuid.UUID) {
	main, second, foreign := uuid.New(), uuid.New(), uuid.New()
	return Scope{
		Level: LevelCompany, UserID: uuid.New(), BranchID: main,
		CompanyID: uuid.New(), Branches: []uuid.UUID{main, second},
	}, main, second, foreign
}

func TestCompanyScopeStaysInsideTenant(t *testing.T) {
	s, main, second, foreign := companyScope()

	for _, b := range []uuid.UUID{main, second} {
		if !s.CanAccessBranch(b) || !s.CanAccess(Record{BranchID: b}) {
			t.Fatalf("company branch %v must be visible", b)
		}
	}
	if s.CanAccessBranch(foreign) || s.CanAccess(Record{BranchID: foreign}) {
		t.Fatal("another company's branch must stay hidden")
	}
	if _, ok := s.BranchFilter(); ok {
		t.Fatal("company scope has no single-branch filter")
	}
	if set, ok := s.CompanyBranches(); !ok || len(set) != 2 {
		t.Fatalf("company branches = %v %v", set, ok)
	}
	if _, ok := (Scope{Level: LevelBranch, BranchID: main}).CompanyBranches(); ok {
		t.Fatal("only company scopes expose a branch set")
	}
}

func TestCompanyScopeResolveBranch(t *testing.T) {
	s, main, second, foreign := companyScope()

	if b, err := s.ResolveBranch(nil); err != nil || *b != main {
		t.Fatalf("no filter must pin to the active branch, got %v %v", b, err)
	}
	if b, err := s.ResolveBranch(&second); err != nil || *b != second {
		t.Fatalf("company branch must be selectable, got %v %v", b, err)
	}
	if _, err := s.ResolveBranch(&foreign); !errors.Is(err, ErrBranchForbidden) {
		t.Fatalf("foreign branch: got %v", err)
	}

	orphan := s
	orphan.BranchID = foreign
	if _, err := orphan.ResolveBranch(nil); !errors.Is(err, ErrBranchForbidden) {
		t.Fatalf("active branch outside the company must fail closed, got %v", err)
	}

	empty := Scope{Level: LevelCompany, BranchID: main}
	if empty.CanAccessBranch(main) {
		t.Fatal("a company scope without a branch set grants nothing")
	}
}

func TestCanAccessCompany(t *testing.T) {
	s, _, _, _ := companyScope()
	if !s.CanAccessCompany(s.CompanyID) {
		t.Fatal("own company must be visible")
	}
	if s.CanAccessCompany(uuid.New()) || s.CanAccessCompany(uuid.Nil) {
		t.Fatal("other or unknown company must stay hidden")
	}
	member := Scope{Level: LevelOwn, CompanyID: s.CompanyID}
	if !member.CanAccessCompany(s.CompanyID) {
		t.Fatal("members read their own company")
	}
	if (Scope{CompanyID: s.CompanyID}).CanAccessCompany(s.CompanyID) {
		t.Fatal("zero scope grants nothing")
	}
	if !System().CanAccessCompany(uuid.New()) {
		t.Fatal("system reaches every company")
	}
}

func TestCompanyLevelOrdering(t *testing.T) {
	if !(LevelBranch < LevelCompany && LevelCompany < LevelGlobal) {
		t.Fatal("company sits between branch and global")
	}
	if LevelCompany.String() != "company" {
		t.Fatalf("string = %q", LevelCompany.String())
	}
	if !(Scope{Level: LevelCompany}).IsElevated() {
		t.Fatal("company scope is elevated")
	}
}
