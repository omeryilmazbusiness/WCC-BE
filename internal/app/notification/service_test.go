package notification

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func TestDeliveryScope(t *testing.T) {
	branch := uuid.New()
	user := uuid.New()
	company := uuid.New()

	cases := []struct {
		name string
		in   access.Scope
		want access.Scope
	}{
		{"none stays denied", access.Scope{}, access.Scope{}},
		{"global becomes system", access.Scope{Level: access.LevelGlobal, UserID: user, BranchID: branch}, access.System()},
		{"branch drops user", access.Scope{Level: access.LevelBranch, UserID: user, BranchID: branch}, access.ForBranch(branch)},
		{"team widens to branch without user", access.Scope{Level: access.LevelTeam, UserID: user, BranchID: branch, TeamID: uuid.New()}, access.ForBranch(branch)},
		{"own widens to branch without user", access.Scope{Level: access.LevelOwn, UserID: user, BranchID: branch}, access.ForBranch(branch)},
		{"company drops user and keeps the tenant", access.Scope{Level: access.LevelCompany, UserID: user, BranchID: branch, CompanyID: company, Branches: []uuid.UUID{branch}},
			access.Scope{Level: access.LevelCompany, BranchID: branch, CompanyID: company, Branches: []uuid.UUID{branch}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeliveryScope(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
