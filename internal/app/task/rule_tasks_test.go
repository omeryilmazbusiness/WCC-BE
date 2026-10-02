package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

func TestRuleTasksAreIdempotentAndCloseByRule(t *testing.T) {
	ctx := context.Background()
	tasks := newTaskMem()
	seeder := apptask.NewSeeder(tasks, tx.Nop{})
	branch, conv, owner := uuid.New(), uuid.New(), uuid.New()
	since := time.Now().UTC().Add(-time.Hour)

	for i := 0; i < 2; i++ {
		if err := seeder.EnsureUnansweredTask(ctx, branch, conv, owner, since); err != nil {
			t.Fatal(err)
		}
	}
	open, _ := tasks.ListOpenByRule(ctx, domain.RuleUnansweredMessage, apptask.RelatedConversation, conv)
	if len(open) != 1 || open[0].Priority != domain.PriorityCritical {
		t.Fatalf("want one urgent unanswered task, got %+v", open)
	}

	n, err := seeder.CloseByRule(ctx, domain.RuleUnansweredMessage, apptask.RelatedConversation, conv, "replied")
	if err != nil || n != 1 {
		t.Fatalf("closed=%d err=%v", n, err)
	}
	got, _ := tasks.FindByID(ctx, open[0].ID)
	if got.Status != domain.StatusDone || got.Outcome != "replied" {
		t.Fatalf("task not closed: %+v", got)
	}
	if n, _ := seeder.CloseByRule(ctx, domain.RuleUnansweredMessage, apptask.RelatedConversation, conv, "replied"); n != 0 {
		t.Fatalf("second close must be a no-op, closed %d", n)
	}
}

func TestSeededTasksCarrySourceRule(t *testing.T) {
	ctx := context.Background()
	tasks := newTaskMem()
	seeder := apptask.NewSeeder(tasks, tx.Nop{})
	branch, owner := uuid.New(), uuid.New()
	cases := map[string]func() error{
		domain.RuleVisaFollowUp:    func() error { return seeder.EnsureVisaFollowUpTask(ctx, branch, uuid.New(), owner, "V-1") },
		domain.RuleSupplierConfirm: func() error { return seeder.EnsureSupplierConfirmTask(ctx, branch, uuid.New(), owner, "Hotel") },
		domain.RuleMissingDocument: func() error {
			return seeder.EnsureMissingDocTask(ctx, branch, uuid.New(), owner, []string{"passport"})
		},
		domain.RulePaymentDue: func() error {
			return seeder.EnsurePaymentDueTask(ctx, branch, uuid.New(), owner, time.Now().UTC(), 100, "USD")
		},
	}
	for rule, ensure := range cases {
		if err := ensure(); err != nil {
			t.Fatalf("%s: %v", rule, err)
		}
	}
	seen := map[string]bool{}
	for _, task := range tasks.byID {
		seen[task.SourceRule] = true
		if task.CreatedBy != nil {
			t.Fatalf("automation tasks have no creator: %+v", task)
		}
	}
	for rule := range cases {
		if !seen[rule] {
			t.Fatalf("no task with source_rule %s", rule)
		}
	}
}

type captureOutbox struct{ events []events.Event }

func (c *captureOutbox) Add(_ context.Context, ev events.Event) error {
	c.events = append(c.events, ev)
	return nil
}

func TestAnnounceOverdueRecordsOncePerEpisode(t *testing.T) {
	ctx := access.WithScope(context.Background(), access.System())
	tasks := newTaskMem()
	svc := apptask.NewService(tasks, tx.Nop{}, events.NewBus(nil))
	out := &captureOutbox{}
	svc.SetOutbox(out)
	past := time.Now().UTC().Add(-time.Hour)
	id := uuid.New()
	_ = tasks.Create(ctx, &domain.Task{
		ID: id, BranchID: uuid.New(), Title: "Call", Kind: domain.KindFollowUp, Status: domain.StatusOpen,
		AssigneeID: uuid.New(), RelatedType: "lead", RelatedID: uuid.New(), DueAt: &past,
	})
	if n, err := svc.AnnounceOverdue(ctx, 10); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, _ := svc.AnnounceOverdue(ctx, 10); n != 0 {
		t.Fatalf("second sweep must not re-announce, got %d", n)
	}
	if len(out.events) != 1 || out.events[0].Name != events.TaskOverdue {
		t.Fatalf("events: %+v", out.events)
	}
	p, ok := out.events[0].Payload.(events.TaskOverduePayload)
	if !ok || p.TaskID != id {
		t.Fatalf("payload: %+v", out.events[0].Payload)
	}
}

type fixedGrace struct {
	grace   time.Duration
	enabled bool
}

func (g fixedGrace) Grace(context.Context, uuid.UUID, string) (time.Duration, bool) {
	return g.grace, g.enabled
}

func TestEscalateOverdueUsesGracePolicy(t *testing.T) {
	ctx := access.WithScope(context.Background(), access.System())
	due := time.Now().UTC().Add(-3 * time.Hour)
	seed := func() (*taskMem, uuid.UUID) {
		tasks := newTaskMem()
		id := uuid.New()
		_ = tasks.Create(ctx, &domain.Task{
			ID: id, BranchID: uuid.New(), Title: "Reply", Kind: domain.KindFollowUp, Status: domain.StatusOpen,
			AssigneeID: uuid.New(), RelatedType: "conversation", RelatedID: uuid.New(), DueAt: &due,
			SourceRule: domain.RuleUnansweredMessage,
		})
		return tasks, id
	}

	tasks, _ := seed()
	svc := apptask.NewService(tasks, tx.Nop{}, events.NewBus(nil))
	if n, _ := svc.EscalateOverdue(ctx, nil); n != 1 {
		t.Fatalf("unanswered rule grace is 2h, 3h overdue must escalate; got %d", n)
	}

	tasks, _ = seed()
	svc = apptask.NewService(tasks, tx.Nop{}, events.NewBus(nil))
	svc.SetGracePolicy(fixedGrace{grace: 4 * time.Hour, enabled: true})
	if n, _ := svc.EscalateOverdue(ctx, nil); n != 0 {
		t.Fatalf("4h branch grace must hold escalation; got %d", n)
	}

	tasks, _ = seed()
	svc = apptask.NewService(tasks, tx.Nop{}, events.NewBus(nil))
	svc.SetGracePolicy(fixedGrace{grace: time.Minute, enabled: false})
	if n, _ := svc.EscalateOverdue(ctx, nil); n != 0 {
		t.Fatalf("disabled rule must never escalate; got %d", n)
	}
}

func TestGraceDefaultsPerRule(t *testing.T) {
	if domain.Grace(domain.RuleUnansweredMessage, nil) != 2*time.Hour {
		t.Fatal("unanswered grace")
	}
	if domain.Grace("", nil) != domain.DefaultGrace {
		t.Fatal("manual grace")
	}
	if domain.Grace("", map[string]time.Duration{"": time.Hour}) != time.Hour {
		t.Fatal("override grace")
	}
}
