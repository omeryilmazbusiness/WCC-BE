package useradmin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type memUsers struct {
	identity.Repository
	users      map[uuid.UUID]identity.User
	lastFilter identity.UserFilter
}

func (m *memUsers) FindUserByID(_ context.Context, id uuid.UUID) (*identity.User, error) {
	u, ok := m.users[id]
	if !ok {
		return nil, shared.NewNotFound("user")
	}
	return &u, nil
}

func (m *memUsers) ListUsers(_ context.Context, f identity.UserFilter) ([]identity.User, int, error) {
	m.lastFilter = f
	return nil, 0, nil
}

func TestPlatformOperatorManagesGMsOnly(t *testing.T) {
	branch := uuid.New()
	gm := identity.User{ID: uuid.New(), Role: platformauth.RoleGM, BranchID: branch}
	staff := identity.User{ID: uuid.New(), Role: platformauth.RoleEmployee, BranchID: branch}
	repo := &memUsers{users: map[uuid.UUID]identity.User{gm.ID: gm, staff.ID: staff}}
	svc := NewService(repo, nil, nil, nil)
	platform := access.WithScope(context.Background(), access.Scope{Level: access.LevelGlobal})

	if _, _, err := svc.List(platform, identity.UserFilter{}); err != nil || repo.lastFilter.Role == nil || *repo.lastFilter.Role != platformauth.RoleGM {
		t.Fatalf("platform listing must be limited to GMs: %v %+v", err, repo.lastFilter)
	}
	repo.lastFilter = identity.UserFilter{}
	other := platformauth.RoleEmployee
	if items, total, err := svc.List(platform, identity.UserFilter{Role: &other}); err != nil || len(items) != 0 || total != 0 || repo.lastFilter.Role != nil {
		t.Fatalf("filtering for another role must return nothing without querying: %v %+v", err, repo.lastFilter)
	}
	if _, err := svc.Get(platform, gm.ID); err != nil {
		t.Fatalf("platform sees GMs: %v", err)
	}
	if _, err := svc.Get(platform, staff.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("company staff must be hidden from the platform, got %v", err)
	}
	_, err := svc.Create(platform, CreateInput{
		Email: "ops@wodi.local", FullName: "Ops", Password: "ChangeMe123!", Role: platformauth.RoleAdmin,
	})
	if !errors.Is(err, shared.ErrForbidden) {
		t.Fatalf("platform may not create non-GM accounts, got %v", err)
	}

	company := access.WithScope(context.Background(), access.Scope{
		Level: access.LevelCompany, BranchID: branch, CompanyID: uuid.New(), Branches: []uuid.UUID{branch},
	})
	repo.lastFilter = identity.UserFilter{}
	if _, _, err := svc.List(company, identity.UserFilter{}); err != nil || repo.lastFilter.Role != nil {
		t.Fatalf("GM listing is not role-limited: %v %+v", err, repo.lastFilter)
	}
	if _, err := svc.Get(company, staff.ID); err != nil {
		t.Fatalf("GM sees own staff: %v", err)
	}
}
