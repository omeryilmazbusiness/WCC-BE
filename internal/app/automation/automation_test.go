package automation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	documentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	inboxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	notificationdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	supplierdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memLedger map[string]bool

func (l memLedger) Claim(_ context.Context, job, key, period string) (bool, error) {
	k := job + "|" + key + "|" + period
	if l[k] {
		return false, nil
	}
	l[k] = true
	return true, nil
}

type sentNote struct {
	to    uuid.UUID
	roles []string
	in    appnotification.EmitInput
}

type fakeNotifier struct{ sent []sentNote }

func (f *fakeNotifier) Emit(_ context.Context, in appnotification.EmitInput) (*notificationdomain.Notification, error) {
	f.sent = append(f.sent, sentNote{to: in.RecipientUserID, in: in})
	return &notificationdomain.Notification{}, nil
}

func (f *fakeNotifier) EmitToRoles(_ context.Context, _ uuid.UUID, roles []string, in appnotification.EmitInput) (int, error) {
	f.sent = append(f.sent, sentNote{roles: roles, in: in})
	return 1, nil
}

func (f *fakeNotifier) kinds() []string {
	out := make([]string, 0, len(f.sent))
	for _, s := range f.sent {
		out = append(out, s.in.Kind)
	}
	return out
}

type fakeTasks struct {
	ensured []string
	closed  []string
}

func (f *fakeTasks) EnsureUnansweredTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error {
	f.ensured = append(f.ensured, taskdomain.RuleUnansweredMessage)
	return nil
}
func (f *fakeTasks) EnsureVisaFollowUpTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	f.ensured = append(f.ensured, taskdomain.RuleVisaFollowUp)
	return nil
}
func (f *fakeTasks) EnsureSupplierConfirmTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error {
	f.ensured = append(f.ensured, taskdomain.RuleSupplierConfirm)
	return nil
}
func (f *fakeTasks) EnsureMissingDocTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []string) error {
	f.ensured = append(f.ensured, taskdomain.RuleMissingDocument)
	return nil
}
func (f *fakeTasks) EnsureTargetRecoveryTask(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, int64) error {
	f.ensured = append(f.ensured, taskdomain.RuleTargetRecovery)
	return nil
}
func (f *fakeTasks) CloseByRule(_ context.Context, rule, _ string, _ uuid.UUID, _ string) (int, error) {
	f.closed = append(f.closed, rule)
	return 1, nil
}

type fakeStaff struct{ id uuid.UUID }

func (s fakeStaff) ListActiveByRoles(context.Context, uuid.UUID, []string) ([]appnotification.UserRef, error) {
	if s.id == uuid.Nil {
		return nil, nil
	}
	return []appnotification.UserRef{{ID: s.id}}, nil
}

type recomputes struct{ bookings, targets int }

func (r *recomputes) RecomputeBooking(context.Context, uuid.UUID) error { r.bookings++; return nil }
func (r *recomputes) RecomputeTargets(context.Context, uuid.UUID) error { r.targets++; return nil }

type fakeChecklist struct{ missing []string }

func (f fakeChecklist) Checklist(_ context.Context, id uuid.UUID) (*documentdomain.Checklist, error) {
	return &documentdomain.Checklist{BookingID: id, MissingRequired: f.missing}, nil
}

type fixture struct {
	notes  *fakeNotifier
	tasks  *fakeTasks
	rec    *recomputes
	alerts *Alerts
	bus    *events.Bus
}

func newFixture(missing []string) fixture {
	f := fixture{notes: &fakeNotifier{}, tasks: &fakeTasks{}, rec: &recomputes{}, bus: events.NewBus(nil)}
	f.alerts = NewAlerts(f.notes, f.tasks, fakeStaff{id: uuid.New()}, memLedger{}, tx.Nop{})
	NewReactor(f.alerts, f.tasks, f.rec, f.rec, fakeChecklist{missing: missing}).Register(f.bus)
	return f
}

func deliver(t *testing.T, bus *events.Bus, name string, payload any) {
	t.Helper()
	if err := bus.Deliver(context.Background(), events.Event{Name: name, Payload: payload}); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func TestPaymentReversedRecomputesBookingAndTargets(t *testing.T) {
	f := newFixture(nil)
	deliver(t, f.bus, events.PaymentReversed, events.PaymentReversedPayload{BookingID: uuid.New(), BranchID: uuid.New()})
	if f.rec.bookings != 1 || f.rec.targets != 1 {
		t.Fatalf("recomputes = %+v", f.rec)
	}
}

func TestDocumentStatusChangedClosesTasksOnlyWhenChecklistComplete(t *testing.T) {
	booking := uuid.New()
	incomplete := newFixture([]string{"passport"})
	deliver(t, incomplete.bus, events.DocumentStatusChanged, events.DocumentStatusChangedPayload{BookingID: &booking, To: "approved"})
	if incomplete.rec.bookings != 1 || len(incomplete.tasks.closed) != 0 {
		t.Fatalf("incomplete: recomputes=%d closed=%v", incomplete.rec.bookings, incomplete.tasks.closed)
	}

	complete := newFixture(nil)
	deliver(t, complete.bus, events.DocumentStatusChanged, events.DocumentStatusChangedPayload{BookingID: &booking, To: "approved"})
	if got := strings.Join(complete.tasks.closed, ","); got != taskdomain.RuleMissingDocument+","+taskdomain.RuleBookingDocuments {
		t.Fatalf("closed = %s", got)
	}

	noBooking := newFixture(nil)
	deliver(t, noBooking.bus, events.DocumentStatusChanged, events.DocumentStatusChangedPayload{To: "approved"})
	if noBooking.rec.bookings != 0 {
		t.Fatal("documents without a booking must not recompute")
	}
}

func TestConversationEventsDriveUnansweredTask(t *testing.T) {
	f := newFixture(nil)
	owner := uuid.New()
	conv := &inboxdomain.Conversation{ID: uuid.New(), BranchID: uuid.New(), OwnerID: &owner, Status: inboxdomain.StatusResolved}
	deliver(t, f.bus, events.SLABreached, conv)
	deliver(t, f.bus, events.ConversationResponded, events.ConversationRespondedPayload{ConversationID: conv.ID})
	deliver(t, f.bus, events.ConversationResolved, conv)
	if len(f.tasks.ensured) != 1 || len(f.tasks.closed) != 2 {
		t.Fatalf("ensured=%v closed=%v", f.tasks.ensured, f.tasks.closed)
	}
	deliver(t, f.bus, events.SLAWarning, conv)
	if k := f.notes.kinds(); len(k) != 1 || k[0] != notificationdomain.KindMessageSLAWarning || f.notes.sent[0].to != owner {
		t.Fatalf("sla warning notes = %+v", f.notes.sent)
	}
}

func TestTargetBehindNotifiesOncePerDayAndOpensRecovery(t *testing.T) {
	f := newFixture(nil)
	p := events.TargetStatusChangedPayload{TargetID: uuid.New(), BranchID: uuid.New(), Label: "Q3", From: "on_track", To: "behind", Deficit: 500}
	deliver(t, f.bus, events.TargetStatusChanged, p)
	deliver(t, f.bus, events.TargetStatusChanged, p)
	if n := len(f.notes.sent); n != 1 {
		t.Fatalf("notifications = %d, want 1", n)
	}
	if len(f.tasks.ensured) != 2 {
		t.Fatalf("recovery ensures = %v", f.tasks.ensured)
	}
	deliver(t, f.bus, events.TargetStatusChanged, events.TargetStatusChangedPayload{TargetID: uuid.New(), To: "on_track"})
	if len(f.notes.sent) != 1 {
		t.Fatal("recovering targets must not notify")
	}
}

func TestLeadClosedAndNotificationEvents(t *testing.T) {
	f := newFixture(nil)
	deliver(t, f.bus, events.LeadStageChanged, events.LeadStageChangedPayload{LeadID: uuid.New(), To: "qualified"})
	deliver(t, f.bus, events.LeadStageChanged, events.LeadStageChangedPayload{LeadID: uuid.New(), To: "won"})
	if len(f.tasks.closed) != 1 || f.tasks.closed[0] != taskdomain.RuleLeadFollowUp {
		t.Fatalf("closed = %v", f.tasks.closed)
	}
	actor := uuid.New()
	deliver(t, f.bus, events.TaskOverdue, events.TaskOverduePayload{TaskID: uuid.New(), AssigneeID: uuid.New(), Title: "Call"})
	deliver(t, f.bus, events.IntegrationFailed, events.IntegrationFailedPayload{BranchID: uuid.New(), Provider: "whatsapp", Error: "401"})
	deliver(t, f.bus, events.ImportCompleted, events.ImportCompletedPayload{ImportJobID: uuid.New(), ActorID: actor, Status: "completed", Inserted: 3})
	want := []string{notificationdomain.KindTaskOverdue, notificationdomain.KindIntegrationDown, notificationdomain.KindImportCompleted}
	if got := strings.Join(f.notes.kinds(), ","); got != strings.Join(want, ",") {
		t.Fatalf("kinds = %s", got)
	}
	if f.notes.sent[2].to != actor || f.notes.sent[2].in.Body != "Imported 3 rows, 0 failed" {
		t.Fatalf("import note = %+v", f.notes.sent[2])
	}
}

func TestPaymentDueAlertOncePerDueDate(t *testing.T) {
	f := newFixture(nil)
	booking, due := uuid.New(), time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		if err := f.alerts.PaymentDue(context.Background(), uuid.New(), booking, uuid.New(), due, 1000, "USD"); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.alerts.PaymentDue(context.Background(), uuid.New(), booking, uuid.New(), due.AddDate(0, 1, 0), 1000, "USD"); err != nil {
		t.Fatal(err)
	}
	if len(f.notes.sent) != 2 {
		t.Fatalf("payment due notes = %d, want 2", len(f.notes.sent))
	}
}

func TestSupplierFollowUpWithoutStaffStillAlerts(t *testing.T) {
	notes, tasks := &fakeNotifier{}, &fakeTasks{}
	a := NewAlerts(notes, tasks, fakeStaff{}, memLedger{}, tx.Nop{})
	link := fakeLink()
	if err := a.SupplierUnconfirmed(context.Background(), fakeSupplier(), link); err != nil {
		t.Fatal(err)
	}
	if len(tasks.ensured) != 0 || len(notes.sent) != 1 {
		t.Fatalf("ensured=%v notes=%d", tasks.ensured, len(notes.sent))
	}
	if err := a.SupplierConfirmed(context.Background(), link); err != nil {
		t.Fatal(err)
	}
	if len(tasks.closed) != 1 || tasks.closed[0] != taskdomain.RuleSupplierConfirm {
		t.Fatalf("closed = %v", tasks.closed)
	}
}

// --- jobs ---

type fakeQueries struct {
	branches  []Branch
	passports []PassportRef
	leads     []LeadRef
	bookings  []BookingRef
	overdue   []OverdueScheduleRef
}

func (q fakeQueries) Branches(context.Context) ([]Branch, error) { return q.branches, nil }
func (q fakeQueries) BookingsAwaitingDocs(context.Context, time.Time, int) ([]BookingRef, error) {
	return q.bookings, nil
}
func (q fakeQueries) IdleLeads(context.Context, time.Time, int) ([]LeadRef, error) {
	return q.leads, nil
}
func (q fakeQueries) ExpiringPassports(context.Context, time.Time, time.Time, int) ([]PassportRef, error) {
	return q.passports, nil
}
func (q fakeQueries) OverdueSchedules(context.Context, time.Time, int) ([]OverdueScheduleRef, error) {
	return q.overdue, nil
}

type fakeAI struct{ calls int }

func (a *fakeAI) DailySummary(context.Context, uuid.UUID, uuid.UUID) (map[string]any, error) {
	a.calls++
	return map[string]any{"headline": "Good morning", "bullets": []string{"Open leads: 3"}}, nil
}

type fakeTaskRead struct{ t *taskdomain.Task }

func (f fakeTaskRead) FindByID(context.Context, uuid.UUID) (*taskdomain.Task, error) {
	if f.t == nil {
		return nil, shared.NewNotFound("task")
	}
	return f.t, nil
}

func newJobs(f fixture, q fakeQueries, ai DailySummarizer, tr TaskReader, now time.Time) *Jobs {
	j := NewJobs(JobDeps{Alerts: f.alerts, Tasks: f.tasks, Queries: q, Checklist: fakeChecklist{missing: []string{"visa"}}, AI: ai, TaskRead: tr})
	j.now = func() time.Time { return now }
	f.alerts.now = j.now
	return j
}

func TestAISummaryRunsOncePerLocalDayAfterSeven(t *testing.T) {
	riyadh, tokyo := Branch{ID: uuid.New(), TimeZone: "Asia/Riyadh"}, Branch{ID: uuid.New(), TimeZone: "Asia/Tokyo"}
	f := newFixture(nil)
	ai := &fakeAI{}
	// 03:30 UTC is 06:30 in Riyadh and 12:30 in Tokyo.
	early := time.Date(2026, 9, 16, 3, 30, 0, 0, time.UTC)
	j := newJobs(f, fakeQueries{branches: []Branch{riyadh, tokyo}}, ai, nil, early)
	if err := j.AISummary(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if ai.calls != 1 {
		t.Fatalf("calls at 03:30 UTC = %d, want 1 (Tokyo only)", ai.calls)
	}
	j.now = func() time.Time { return early.Add(time.Hour) }
	if err := j.AISummary(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if ai.calls != 2 {
		t.Fatalf("calls at 04:30 UTC = %d, want 2 (Riyadh joins, Tokyo deduped)", ai.calls)
	}
	if f.notes.sent[0].in.Kind != notificationdomain.KindAISummary || f.notes.sent[0].in.Body != "Open leads: 3" {
		t.Fatalf("summary note = %+v", f.notes.sent[0].in)
	}
}

func TestPassportExpiryOncePerSlot(t *testing.T) {
	now := time.Date(2026, 9, 16, 6, 0, 0, 0, time.UTC)
	ref := PassportRef{CustomerID: uuid.New(), BranchID: uuid.New(), FullName: "Ali", OwnerID: uuid.New(), ExpiresAt: now.AddDate(0, 0, 100)}
	f := newFixture(nil)
	j := newJobs(f, fakeQueries{passports: []PassportRef{ref}}, nil, nil, now)
	for i := 0; i < 2; i++ {
		if err := j.PassportExpiry(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	j.now = func() time.Time { return now.AddDate(0, 0, 15) } // 85 days left: next slot
	if err := j.PassportExpiry(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(f.notes.sent) != 2 {
		t.Fatalf("passport notes = %d, want 2", len(f.notes.sent))
	}
}

func TestPassportSlot(t *testing.T) {
	for days, want := range map[int]int{200: 180, 180: 180, 120: 180, 90: 90, 31: 90, 30: 30, 0: 30, -1: 0} {
		if got := passportSlot(days); got != want {
			t.Fatalf("passportSlot(%d) = %d, want %d", days, got, want)
		}
	}
}

func TestIdleLeadsAndMissingDocsDedupeDaily(t *testing.T) {
	now := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	q := fakeQueries{
		leads:    []LeadRef{{ID: uuid.New(), BranchID: uuid.New(), OwnerID: uuid.New(), FullName: "Omar"}},
		bookings: []BookingRef{{ID: uuid.New(), BranchID: uuid.New(), OwnerID: uuid.New(), DepartureCode: "UMR-01"}},
	}
	f := newFixture(nil)
	j := newJobs(f, q, nil, nil, now)
	for i := 0; i < 3; i++ {
		if err := j.IdleLeads(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
		if err := j.MissingDocs(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if got := strings.Join(f.notes.kinds(), ","); got != notificationdomain.KindLeadNoFollowUp+","+notificationdomain.KindDocumentMissing {
		t.Fatalf("kinds = %s", got)
	}
	if len(f.tasks.ensured) != 3 {
		t.Fatalf("missing-doc task ensures = %d, want 3 (idempotent in the seeder)", len(f.tasks.ensured))
	}
}

func TestReminderOnlyForOpenTasks(t *testing.T) {
	f := newFixture(nil)
	open := &taskdomain.Task{ID: uuid.New(), AssigneeID: uuid.New(), Status: taskdomain.StatusOpen, Title: "Call back"}
	done := &taskdomain.Task{ID: uuid.New(), AssigneeID: uuid.New(), Status: taskdomain.StatusDone}
	payload := []byte(`{"task_id":"` + uuid.NewString() + `"}`)
	for _, tr := range []TaskReader{fakeTaskRead{t: open}, fakeTaskRead{t: done}, fakeTaskRead{}} {
		if err := newJobs(f, fakeQueries{}, nil, tr, time.Now()).Reminder(context.Background(), payload); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.notes.sent) != 1 || f.notes.sent[0].in.Kind != notificationdomain.KindTaskReminder {
		t.Fatalf("reminders = %+v", f.notes.sent)
	}
	if err := newJobs(f, fakeQueries{}, nil, fakeTaskRead{}, time.Now()).Reminder(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("expected error for missing task_id")
	}
}

func TestEveryAutomationJobHasAHandler(t *testing.T) {
	h := NewJobs(JobDeps{}).Handlers()
	for _, name := range []shared.JobName{
		JobEscalations, JobPaymentDue, JobPaymentOverdue, JobDocumentExpiry, JobPassportExpiry, JobSupplierConfirm, JobVisaFollowUp,
		JobTargetRecompute, JobMissingDocs, JobIdleLeads, JobOutboxPurge, shared.JobSLASweep, shared.JobAISummaryDaily,
		shared.JobReportGenerate, shared.JobWebhookRetry, shared.JobReminderSend, taskdomain.JobOverdueSweep, taskdomain.JobEscalateOverdue,
	} {
		if h[name] == nil {
			t.Fatalf("no handler for %s", name)
		}
	}
}

// --- grace ---

type fakeRules struct{ rules []notificationdomain.Rule }

func (f fakeRules) EscalationRules(context.Context, uuid.UUID) ([]notificationdomain.Rule, error) {
	return f.rules, nil
}

func TestTaskGraceFollowsBranchRule(t *testing.T) {
	def := *notificationdomain.MatchRule(notificationdomain.KindTaskOverdue)
	branch := uuid.New()

	g := TaskGrace{Rules: fakeRules{rules: []notificationdomain.Rule{def}}}
	if d, ok := g.Grace(context.Background(), branch, ""); !ok || d != taskdomain.DefaultGrace {
		t.Fatalf("default manual grace = %v %v", d, ok)
	}
	custom := def
	custom.EscalateAfter = 6 * time.Hour
	g = TaskGrace{Rules: fakeRules{rules: []notificationdomain.Rule{custom}}}
	if d, _ := g.Grace(context.Background(), branch, ""); d != 6*time.Hour {
		t.Fatalf("overridden manual grace = %v", d)
	}
	if d, _ := g.Grace(context.Background(), branch, taskdomain.RuleUnansweredMessage); d != 2*time.Hour {
		t.Fatalf("rule grace = %v", d)
	}
	g = TaskGrace{Rules: fakeRules{}}
	if _, ok := g.Grace(context.Background(), branch, ""); ok {
		t.Fatal("disabled task.overdue rule must stop escalation")
	}
}

func fakeSupplier() *supplierdomain.Supplier {
	return &supplierdomain.Supplier{ID: uuid.New(), BranchID: uuid.New(), NameEn: "Hilton"}
}

func fakeLink() *supplierdomain.Link {
	return &supplierdomain.Link{ID: uuid.New(), LinkType: "hotel"}
}

func TestPaymentOverdueAlertsOncePerSchedule(t *testing.T) {
	now := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)
	owner := uuid.New()
	first := OverdueScheduleRef{ScheduleID: uuid.New(), BookingID: uuid.New(), BranchID: uuid.New(), OwnerID: owner,
		DueAt: now.Add(-20 * time.Hour), Amount: 150000, Currency: "USD"}
	second := first
	second.ScheduleID = uuid.New()
	f := newFixture(nil)
	j := newJobs(f, fakeQueries{overdue: []OverdueScheduleRef{first, second}}, nil, nil, now)
	for i := 0; i < 3; i++ {
		j.now = func() time.Time { return now.AddDate(0, 0, i) }
		if err := j.PaymentOverdue(context.Background(), nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.notes.sent) != 2 {
		t.Fatalf("overdue notes = %d, want one per schedule", len(f.notes.sent))
	}
	n := f.notes.sent[0]
	if n.in.Kind != notificationdomain.KindPaymentOverdue || n.in.RecipientUserID != owner || n.in.Body != "1500.00 USD was due on 2026-09-15" {
		t.Fatalf("note = %+v", n.in)
	}
}
