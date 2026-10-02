package task_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

func TestTaskTransitions(t *testing.T) {
	task := &domain.Task{Status: domain.StatusOpen}
	if err := task.TransitionTo(domain.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := task.TransitionTo(domain.StatusDone); err != nil {
		t.Fatal(err)
	}
	if task.CompletedAt == nil {
		t.Fatal("expected completed_at")
	}
	if err := task.TransitionTo(domain.StatusOpen); err == nil {
		t.Fatal("done is terminal")
	}
}

func TestReschedule(t *testing.T) {
	task := &domain.Task{Status: domain.StatusOpen}
	due := time.Now().UTC().Add(24 * time.Hour)
	if err := task.Reschedule(&due); err != nil {
		t.Fatal(err)
	}
	task.Status = domain.StatusDone
	if err := task.Reschedule(&due); err == nil {
		t.Fatal("expected error")
	} else {
		var app *shared.AppError
		if !errors.As(err, &app) {
			t.Fatalf("want AppError, got %v", err)
		}
	}
}

func TestEscalateAfterGrace(t *testing.T) {
	due := time.Now().UTC().Add(-25 * time.Hour)
	task := &domain.Task{Status: domain.StatusOpen, DueAt: &due, Priority: domain.PriorityMinor}
	now := time.Now().UTC()
	if !task.ShouldEscalate(now, domain.DefaultGrace) {
		t.Fatal("expected escalate after grace")
	}
	task.Escalate(now)
	if task.EscalatedAt == nil || task.Priority != domain.PriorityMajor {
		t.Fatalf("escalate failed %#v", task)
	}
}

func TestAssignClosedRejected(t *testing.T) {
	task := &domain.Task{Status: domain.StatusDone}
	if err := task.Assign(uuid.New()); err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePriorityScaleAndLegacyValues(t *testing.T) {
	cases := map[string]domain.Priority{
		"": domain.PriorityMinor, "minor": domain.PriorityMinor, " Major ": domain.PriorityMajor,
		"critical": domain.PriorityCritical, "low": domain.PriorityMinor, "normal": domain.PriorityMinor,
		"high": domain.PriorityMajor, "urgent": domain.PriorityCritical,
	}
	for in, want := range cases {
		got, err := domain.ParsePriority(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %q err %v, want %q", in, got, err, want)
		}
	}
	if _, err := domain.ParsePriority("blocker"); err == nil {
		t.Fatal("unknown priority must be rejected")
	}
	if domain.ValidPriority("high") {
		t.Fatal("retired values are only accepted through ParsePriority")
	}
}

func TestValidateRelatedPair(t *testing.T) {
	id := uuid.New()
	if err := domain.ValidateRelated("", uuid.Nil); err != nil {
		t.Fatalf("standalone task must be allowed: %v", err)
	}
	if err := domain.ValidateRelated("lead", id); err != nil {
		t.Fatalf("linked task must be allowed: %v", err)
	}
	if domain.ValidateRelated("lead", uuid.Nil) == nil || domain.ValidateRelated(" ", id) == nil {
		t.Fatal("half-filled link must be rejected")
	}
}

func TestNormalizeTitleAndNewDue(t *testing.T) {
	if got, err := domain.NormalizeTitle("  Call back  "); err != nil || got != "Call back" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := domain.NormalizeTitle(" "); err == nil {
		t.Fatal("empty title must be rejected")
	}
	now := time.Now().UTC()
	soon, slightlyPast, past := now.Add(time.Hour), now.Add(-time.Minute), now.Add(-time.Hour)
	if domain.ValidateNewDue(nil, now) != nil || domain.ValidateNewDue(&soon, now) != nil || domain.ValidateNewDue(&slightlyPast, now) != nil {
		t.Fatal("no deadline, a future one and small clock skew must pass")
	}
	if domain.ValidateNewDue(&past, now) == nil {
		t.Fatal("a deadline in the past must be rejected")
	}
}
