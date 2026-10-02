package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type fakeDirectory map[uuid.UUID]string

func (f fakeDirectory) CompanySlugOfBranch(_ context.Context, branchID uuid.UUID) (string, error) {
	slug, ok := f[branchID]
	if !ok {
		return "", shared.NewNotFound("company")
	}
	return slug, nil
}

func TestCompanySignInPage(t *testing.T) {
	h := newHarness(t, platformauth.RoleEmployee)
	h.svc.companies = fakeDirectory{h.user.BranchID: "acme"}
	login := func(company, password string) (*LoginResult, error) {
		return h.svc.Login(context.Background(), LoginInput{
			Email: "user@wodi.test", Password: password, Company: company, IP: "10.0.0.7",
		})
	}

	for _, company := range []string{"", "acme", " ACME "} {
		if res, err := login(company, "correct-horse"); err != nil || res.Tokens == nil {
			t.Fatalf("company %q: %v", company, err)
		}
	}

	_, wrongCompany := login("globex", "correct-horse")
	_, wrongPassword := login("globex", "nope")
	if !errors.Is(wrongCompany, shared.ErrUnauthorized) || wrongCompany.Error() != wrongPassword.Error() {
		t.Fatalf("another company's page must look like bad credentials: %v vs %v", wrongCompany, wrongPassword)
	}
	if !h.audit.has("auth.login_wrong_company") {
		t.Fatal("wrong company sign-in must be audited")
	}

	// A right password on the wrong page is not a guess: it never locks the account.
	for i := 0; i < 10; i++ {
		if _, err := login("globex", "correct-horse"); errors.Is(err, shared.ErrLocked) {
			t.Fatalf("attempt %d locked the account", i)
		}
	}
	if res, err := login("acme", "correct-horse"); err != nil || res.Tokens == nil {
		t.Fatalf("own page after wrong-page attempts: %v", err)
	}

	h.svc.companies = fakeDirectory{}
	if _, err := login("acme", "correct-horse"); !errors.Is(err, shared.ErrUnauthorized) {
		t.Fatalf("a user without a company (platform admin) cannot use a company page: %v", err)
	}
}
