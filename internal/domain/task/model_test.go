package task_test

import (
	"errors"
	"testing"
	"time"

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
