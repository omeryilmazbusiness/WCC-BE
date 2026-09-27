package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
)

type escalatorFunc func(ctx context.Context, requested *uuid.UUID) (int, error)

func (f escalatorFunc) EscalateOverdue(ctx context.Context, requested *uuid.UUID) (int, error) {
	return f(ctx, requested)
}

func TestEscalateOverdueJobRunsAsSystemAcrossBranches(t *testing.T) {
	var gotScope access.Scope
	var gotActor audit.Actor
	var gotBranch *uuid.UUID
	calls := 0
	jobs := &TaskJobs{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		svc: escalatorFunc(func(ctx context.Context, requested *uuid.UUID) (int, error) {
			calls++
			gotScope, gotBranch = access.From(ctx), requested
			gotActor, _ = audit.ActorFrom(ctx)
			return 2, nil
		}),
	}
	if err := jobs.EscalateOverdue(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || gotScope != access.System() || gotBranch != nil || gotActor.Type != audit.ActorSystem {
		t.Fatalf("calls=%d scope=%+v branch=%v actor=%+v", calls, gotScope, gotBranch, gotActor)
	}
}

func TestEscalateOverdueJobSurfacesErrorsForRetry(t *testing.T) {
	boom := errors.New("db down")
	jobs := &TaskJobs{
		log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		svc: escalatorFunc(func(context.Context, *uuid.UUID) (int, error) { return 0, boom }),
	}
	if err := jobs.EscalateOverdue(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("want %v, got %v", boom, err)
	}
}
