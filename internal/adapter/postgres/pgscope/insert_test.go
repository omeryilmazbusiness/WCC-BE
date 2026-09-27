package pgscope

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
)

func TestEnsureBranch(t *testing.T) {
	own, other := uuid.New(), uuid.New()
	emp := access.WithScope(context.Background(), access.Scope{Level: access.LevelOwn, BranchID: own, UserID: uuid.New()})

	if err := EnsureBranch(emp, own); err != nil {
		t.Fatalf("own branch: %v", err)
	}
	if err := EnsureBranch(emp, other); !errors.Is(err, access.ErrBranchForbidden) {
		t.Fatalf("other branch: got %v", err)
	}
	if err := EnsureBranch(access.WithScope(context.Background(), access.System()), other); err != nil {
		t.Fatalf("system: %v", err)
	}
	if err := EnsureBranch(context.Background(), own); err == nil {
		t.Fatal("missing scope allowed")
	}
}
