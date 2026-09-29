package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type staticWorkspaces map[uuid.UUID]*company.Workspace

func (s staticWorkspaces) Workspace(_ context.Context, branchID uuid.UUID) (*company.Workspace, error) {
	ws, ok := s[branchID]
	if !ok {
		return nil, shared.NewNotFound("branch")
	}
	return ws, nil
}

type failingWorkspaces struct{}

func (failingWorkspaces) Workspace(context.Context, uuid.UUID) (*company.Workspace, error) {
	return nil, errors.New("db down")
}

func tenant(active bool) (*company.Workspace, uuid.UUID, uuid.UUID) {
	cid, main, second := uuid.New(), uuid.New(), uuid.New()
	return &company.Workspace{
		Company: company.Company{ID: cid, Slug: "acme", IsActive: active},
		Branches: []company.Branch{
			{ID: main, CompanyID: cid, Kind: company.KindMainCenter},
			{ID: second, CompanyID: cid, Kind: company.KindBranch},
		},
	}, main, second
}

// serve runs Tenancy for a caller of role at home and returns the status and
// the scope the next handler saw.
func serve(t *testing.T, res WorkspaceResolver, role platformauth.Role, home uuid.UUID, header string) (int, access.Scope) {
	t.Helper()
	var seen access.Scope
	h := Tenancy(res)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = access.From(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	claims := &platformauth.Claims{UserID: uuid.New(), Role: role, BranchID: home}
	ctx := context.WithValue(context.Background(), claimsKey, claims)
	ctx = access.WithScope(ctx, platformauth.ScopeFor(claims))
	req := httptest.NewRequest(http.MethodGet, "/v1/leads", nil).WithContext(ctx)
	if header != "" {
		req.Header.Set(BranchHeader, header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, seen
}

func TestTenancyGMSwitchesBranchInsideCompany(t *testing.T) {
	ws, main, second := tenant(true)
	res := staticWorkspaces{main: ws, second: ws}

	code, s := serve(t, res, platformauth.RoleGM, main, "")
	if code != http.StatusNoContent || s.Level != access.LevelCompany || s.BranchID != main || s.CompanyID != ws.Company.ID || len(s.Branches) != 2 {
		t.Fatalf("home branch: %d %+v", code, s)
	}
	code, s = serve(t, res, platformauth.RoleGM, main, second.String())
	if code != http.StatusNoContent || s.BranchID != second {
		t.Fatalf("switch: %d %+v", code, s)
	}
	if code, _ = serve(t, res, platformauth.RoleGM, main, uuid.NewString()); code != http.StatusForbidden {
		t.Fatalf("foreign branch must be refused, got %d", code)
	}
	if code, _ = serve(t, res, platformauth.RoleGM, main, "not-a-uuid"); code != http.StatusForbidden {
		t.Fatalf("malformed branch must be refused, got %d", code)
	}
}

func TestTenancyStaffIgnoreBranchHeader(t *testing.T) {
	ws, main, second := tenant(true)
	code, s := serve(t, staticWorkspaces{main: ws}, platformauth.RoleEmployee, main, second.String())
	if code != http.StatusNoContent || s.BranchID != main || s.CompanyID != ws.Company.ID || len(s.Branches) != 0 {
		t.Fatalf("staff stay on their branch: %d %+v", code, s)
	}
}

func TestTenancyFailsClosed(t *testing.T) {
	ws, main, _ := tenant(false)
	if code, _ := serve(t, staticWorkspaces{main: ws}, platformauth.RoleGM, main, ""); code != http.StatusForbidden {
		t.Fatalf("suspended company: %d", code)
	}
	if code, _ := serve(t, staticWorkspaces{}, platformauth.RoleEmployee, uuid.New(), ""); code != http.StatusForbidden {
		t.Fatalf("branch without company: %d", code)
	}
	if code, _ := serve(t, failingWorkspaces{}, platformauth.RoleEmployee, uuid.New(), ""); code != http.StatusServiceUnavailable {
		t.Fatalf("lookup failure: %d", code)
	}
}
