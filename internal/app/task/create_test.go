package task_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func createCtx(level access.Level) (context.Context, uuid.UUID, uuid.UUID) {
	user, branch := uuid.New(), uuid.New()
	ctx := access.WithScope(context.Background(), access.Scope{Level: level, UserID: user, BranchID: branch})
	return ctx, user, branch
}

func isValidation(err error) bool {
	return errors.Is(err, shared.ErrValidation)
}

func TestCreateStandaloneManualTask(t *testing.T) {
	ctx, user, branch := createCtx(access.LevelOwn)
	tasks := newTaskMem()
	svc := apptask.NewService(tasks, tx.Nop{}, events.NewBus(nil))
	due := time.Now().UTC().Add(3 * time.Hour)

	got, err := svc.Create(ctx, apptask.CreateInput{
		Title: "  Prepare visa checklist  ", Description: "  Include the hotel letter.\n", Kind: domain.KindCustom,
		Priority: domain.PriorityCritical, DueAt: &due,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Prepare visa checklist" || got.Priority != domain.PriorityCritical {
		t.Fatalf("title/priority not kept: %+v", got)
	}
	if got.Description != "Include the hotel letter." {
		t.Fatalf("description not trimmed and kept: %q", got.Description)
	}
	if got.AssigneeID != user || got.BranchID != branch || got.CreatedBy == nil || *got.CreatedBy != user {
		t.Fatalf("ownership not defaulted to caller: %+v", got)
	}
	if got.HasRelated() || got.RelatedType != "" {
		t.Fatalf("standalone task must not link a record: %+v", got)
	}
	if got.Status != domain.StatusOpen || got.SourceRule != "" {
		t.Fatalf("manual task must start open without a rule: %+v", got)
	}
	if stored, _ := tasks.FindByID(ctx, got.ID); stored == nil {
		t.Fatal("task not stored")
	}
}

func TestCreateDefaultsAndLegacyPriorities(t *testing.T) {
	ctx, _, _ := createCtx(access.LevelBranch)
	svc := apptask.NewService(newTaskMem(), tx.Nop{}, events.NewBus(nil))
	cases := map[domain.Priority]domain.Priority{
		"":       domain.PriorityMinor,
		"minor":  domain.PriorityMinor,
		"MAJOR":  domain.PriorityMajor,
		"low":    domain.PriorityMinor,
		"normal": domain.PriorityMinor,
		"high":   domain.PriorityMajor,
		"urgent": domain.PriorityCritical,
	}
	for in, want := range cases {
		got, err := svc.Create(ctx, apptask.CreateInput{Title: "Call", Kind: domain.KindFollowUp, Priority: in})
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got.Priority != want {
			t.Fatalf("%q: want %s, got %s", in, want, got.Priority)
		}
	}
}

func TestCreateLinkedTask(t *testing.T) {
	ctx, _, _ := createCtx(access.LevelBranch)
	svc := apptask.NewService(newTaskMem(), tx.Nop{}, events.NewBus(nil))
	lead := uuid.New()
	got, err := svc.Create(ctx, apptask.CreateInput{
		Title: "Send proposal", Kind: domain.KindFollowUp, Priority: domain.PriorityMajor,
		RelatedType: "lead", RelatedID: lead,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.HasRelated() || got.RelatedType != "lead" || got.RelatedID != lead {
		t.Fatalf("link not kept: %+v", got)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	ctx, _, _ := createCtx(access.LevelBranch)
	svc := apptask.NewService(newTaskMem(), tx.Nop{}, events.NewBus(nil))
	past := time.Now().UTC().Add(-time.Hour)
	cases := map[string]apptask.CreateInput{
		"empty title":      {Title: "   ", Kind: domain.KindCustom},
		"long title":       {Title: strings.Repeat("x", domain.MaxTitleLength+1), Kind: domain.KindCustom},
		"long description": {Title: "x", Kind: domain.KindCustom, Description: strings.Repeat("x", domain.MaxDescriptionLength+1)},
		"bad kind":         {Title: "x", Kind: "meeting"},
		"bad priority":     {Title: "x", Kind: domain.KindCustom, Priority: "blocker"},
		"type without id":  {Title: "x", Kind: domain.KindCustom, RelatedType: "lead"},
		"id without type":  {Title: "x", Kind: domain.KindCustom, RelatedID: uuid.New()},
		"past deadline":    {Title: "x", Kind: domain.KindCustom, DueAt: &past},
	}
	for name, in := range cases {
		if _, err := svc.Create(ctx, in); !isValidation(err) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
}

func TestCreateOwnScopeCannotAssignOthers(t *testing.T) {
	ctx, _, _ := createCtx(access.LevelOwn)
	svc := apptask.NewService(newTaskMem(), tx.Nop{}, events.NewBus(nil))
	if _, err := svc.Create(ctx, apptask.CreateInput{Title: "x", Kind: domain.KindCustom, AssigneeID: uuid.New()}); err == nil {
		t.Fatal("own-level caller must not assign a task to someone else")
	}
}
