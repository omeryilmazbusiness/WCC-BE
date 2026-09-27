package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	notificationdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

// Scheduled automation jobs owned by this package (T-279/T-280).
const (
	JobEscalations     shared.JobName = "notification.escalations"
	JobPaymentDue      shared.JobName = "finance.payment_due"
	JobPaymentOverdue  shared.JobName = "finance.payment_overdue"
	JobDocumentExpiry  shared.JobName = "document.expiry_reminders"
	JobPassportExpiry  shared.JobName = "customer.passport_expiry"
	JobSupplierConfirm shared.JobName = "supplier.confirm_reminders"
	JobVisaFollowUp    shared.JobName = "visa.follow_up"
	JobTargetRecompute shared.JobName = "target.recompute"
	JobMissingDocs     shared.JobName = "booking.missing_docs"
	JobIdleLeads       shared.JobName = "lead.idle_check"
	JobOutboxPurge     shared.JobName = "outbox.purge"
)

// Tunables of the sweeps.
const (
	sweepBatch        = 500
	paymentDueWithin  = 72 * time.Hour
	aiSummaryHour     = 7
	webhookRetryBatch = 100
	reportBatch       = 200
)

// PassportReminderDays are the reminder slots before a passport expires.
var PassportReminderDays = []int{180, 90, 30}

// Ports of the modules the sweeps drive (ISP: one method each).
type (
	SLAChecker interface {
		CheckSLA(ctx context.Context, limit int) (warned, breached int, err error)
	}
	Escalator interface {
		ProcessEscalations(ctx context.Context, now time.Time) (int, error)
	}
	OverdueTasks interface {
		AnnounceOverdue(ctx context.Context, limit int) (int, error)
		EscalateOverdue(ctx context.Context, requested *uuid.UUID) (int, error)
	}
	PaymentDueSweeper interface {
		ProcessPaymentDueReminders(ctx context.Context, within time.Duration, limit int) (int, error)
	}
	DocumentExpirySweeper interface {
		ProcessExpiryReminders(ctx context.Context, now time.Time, limit int) (int, error)
	}
	SupplierSweeper interface {
		ProcessUnconfirmedReminders(ctx context.Context, branchID *uuid.UUID, limit int) (int, error)
	}
	VisaSweeper interface {
		ProcessFollowUps(ctx context.Context, now time.Time, limit int) (int, error)
	}
	TargetSweeper interface {
		CheckBehindAlerts(ctx context.Context, branchID, actorID uuid.UUID) (int, error)
	}
	WebhookRetrier interface {
		RetryFailed(ctx context.Context, limit int) (retried, recovered int, err error)
	}
	ReportRunner interface {
		RunDue(ctx context.Context, limit int) (int, error)
	}
	DailySummarizer interface {
		DailySummary(ctx context.Context, branchID, actorID uuid.UUID) (map[string]any, error)
	}
	OutboxPurger interface {
		Purge(ctx context.Context) (int64, error)
	}
)

// JobDeps are the collaborators of the scheduled handlers; every field is required.
type JobDeps struct {
	Alerts    *Alerts
	Tasks     RuleTasks
	TaskRead  TaskReader
	Queries   Queries
	Checklist Checklists
	SLA       SLAChecker
	Escalate  Escalator
	Overdue   OverdueTasks
	Payments  PaymentDueSweeper
	Documents DocumentExpirySweeper
	Suppliers SupplierSweeper
	Visas     VisaSweeper
	Targets   TargetSweeper
	Webhooks  WebhookRetrier
	Reports   ReportRunner
	AI        DailySummarizer
	Outbox    OutboxPurger
	Log       *slog.Logger
}

// Jobs are the worker handlers of the automation schedule. They run under
// the worker's system scope and are safe to re-run (idempotent per slot).
type Jobs struct {
	d   JobDeps
	now func() time.Time
}

func NewJobs(d JobDeps) *Jobs {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Jobs{d: d, now: func() time.Time { return time.Now().UTC() }}
}

// Handler is the worker handler shape (payload is the raw job payload).
type Handler func(ctx context.Context, payload []byte) error

// Handlers maps every automation job to its handler.
func (j *Jobs) Handlers() map[shared.JobName]Handler {
	return map[shared.JobName]Handler{
		shared.JobSLASweep:            j.SLASweep,
		JobEscalations:                j.Escalations,
		taskdomain.JobOverdueSweep:    j.TaskOverdueSweep,
		taskdomain.JobEscalateOverdue: j.TaskEscalate,
		JobPaymentDue:                 j.PaymentDue,
		JobPaymentOverdue:             j.PaymentOverdue,
		JobDocumentExpiry:             j.DocumentExpiry,
		JobPassportExpiry:             j.PassportExpiry,
		JobSupplierConfirm:            j.SupplierConfirm,
		JobVisaFollowUp:               j.VisaFollowUp,
		JobTargetRecompute:            j.TargetRecompute,
		JobMissingDocs:                j.MissingDocs,
		JobIdleLeads:                  j.IdleLeads,
		shared.JobAISummaryDaily:      j.AISummary,
		shared.JobReportGenerate:      j.Reports,
		shared.JobWebhookRetry:        j.WebhookRetry,
		shared.JobReminderSend:        j.Reminder,
		JobOutboxPurge:                j.OutboxPurge,
	}
}

func (j *Jobs) logCount(ctx context.Context, msg string, n int, err error, attrs ...any) error {
	if n > 0 {
		j.d.Log.InfoContext(ctx, msg, append([]any{"count", n}, attrs...)...)
	}
	return err
}

func (j *Jobs) SLASweep(ctx context.Context, _ []byte) error {
	warned, breached, err := j.d.SLA.CheckSLA(ctx, sweepBatch)
	return j.logCount(ctx, "sla sweep", warned+breached, err, "warned", warned, "breached", breached)
}

func (j *Jobs) Escalations(ctx context.Context, _ []byte) error {
	n, err := j.d.Escalate.ProcessEscalations(ctx, j.now())
	return j.logCount(ctx, "notifications escalated", n, err)
}

func (j *Jobs) TaskOverdueSweep(ctx context.Context, _ []byte) error {
	n, err := j.d.Overdue.AnnounceOverdue(ctx, sweepBatch)
	return j.logCount(ctx, "overdue tasks announced", n, err)
}

func (j *Jobs) TaskEscalate(ctx context.Context, _ []byte) error {
	n, err := j.d.Overdue.EscalateOverdue(ctx, nil)
	return j.logCount(ctx, "overdue tasks escalated", n, err)
}

func (j *Jobs) PaymentDue(ctx context.Context, _ []byte) error {
	n, err := j.d.Payments.ProcessPaymentDueReminders(ctx, paymentDueWithin, sweepBatch)
	return j.logCount(ctx, "payment due reminders", n, err)
}

func (j *Jobs) DocumentExpiry(ctx context.Context, _ []byte) error {
	n, err := j.d.Documents.ProcessExpiryReminders(ctx, j.now(), sweepBatch)
	return j.logCount(ctx, "document expiry actions", n, err)
}

func (j *Jobs) SupplierConfirm(ctx context.Context, _ []byte) error {
	n, err := j.d.Suppliers.ProcessUnconfirmedReminders(ctx, nil, sweepBatch)
	return j.logCount(ctx, "supplier confirmation follow-ups", n, err)
}

func (j *Jobs) VisaFollowUp(ctx context.Context, _ []byte) error {
	n, err := j.d.Visas.ProcessFollowUps(ctx, j.now(), sweepBatch)
	return j.logCount(ctx, "visa follow-ups", n, err)
}

func (j *Jobs) WebhookRetry(ctx context.Context, _ []byte) error {
	retried, recovered, err := j.d.Webhooks.RetryFailed(ctx, webhookRetryBatch)
	return j.logCount(ctx, "webhook retries", retried, err, "recovered", recovered)
}

func (j *Jobs) Reports(ctx context.Context, _ []byte) error {
	n, err := j.d.Reports.RunDue(ctx, reportBatch)
	return j.logCount(ctx, "scheduled reports generated", n, err)
}

func (j *Jobs) OutboxPurge(ctx context.Context, _ []byte) error {
	n, err := j.d.Outbox.Purge(ctx)
	return j.logCount(ctx, "outbox rows purged", int(n), err)
}

// forBranches runs fn for every active branch; one failing branch does not
// starve the others, the joined error makes the job retry.
func (j *Jobs) forBranches(ctx context.Context, fn func(Branch) error) error {
	branches, err := j.d.Queries.Branches(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, b := range branches {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(b); err != nil {
			errs = append(errs, fmt.Errorf("branch %s: %w", b.ID, err))
		}
	}
	return errors.Join(errs...)
}

// TargetRecompute refreshes every branch's targets; behind targets get their
// (daily-deduped) alert and recovery task.
func (j *Jobs) TargetRecompute(ctx context.Context, _ []byte) error {
	behind := 0
	err := j.forBranches(ctx, func(b Branch) error {
		n, err := j.d.Targets.CheckBehindAlerts(ctx, b.ID, uuid.Nil)
		behind += n
		return err
	})
	return j.logCount(ctx, "targets behind", behind, err)
}

// AISummary runs hourly and produces each branch's summary once per local
// day, at or after 07:00 branch time (a missed hour catches up later).
func (j *Jobs) AISummary(ctx context.Context, _ []byte) error {
	now := j.now()
	sent := 0
	err := j.forBranches(ctx, func(b Branch) error {
		local := now.In(b.Location())
		if local.Hour() < aiSummaryHour {
			return nil
		}
		a := j.d.Alerts
		return a.once(ctx, ledgerAISummary, b.ID.String(), local.Format(time.DateOnly), func(ctx context.Context) error {
			out, err := j.d.AI.DailySummary(ctx, b.ID, uuid.Nil)
			if err != nil {
				return err
			}
			id := b.ID
			_, err = a.notify.EmitToRoles(ctx, b.ID, managerRoles, appnotification.EmitInput{
				Kind: notificationdomain.KindAISummary, Title: summaryHeadline(out),
				Body: summaryBody(out), EntityType: "branch", EntityID: &id, HrefHint: "/manager",
			})
			sent++
			return err
		})
	})
	return j.logCount(ctx, "ai daily summaries", sent, err)
}

func summaryHeadline(out map[string]any) string {
	if h, ok := out["headline"].(string); ok && strings.TrimSpace(h) != "" {
		return h
	}
	return "Daily AI summary"
}

func summaryBody(out map[string]any) string {
	switch v := out["bullets"].(type) {
	case []string:
		return strings.Join(v, "\n")
	case []any:
		lines := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				lines = append(lines, s)
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

// PassportExpiry reminds the booking owner of travelling customers whose
// passport expires within 180/90/30 days, once per slot.
func (j *Jobs) PassportExpiry(ctx context.Context, _ []byte) error {
	now := j.now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	items, err := j.d.Queries.ExpiringPassports(ctx, today, today.AddDate(0, 0, PassportReminderDays[0]), sweepBatch)
	if err != nil {
		return err
	}
	a := j.d.Alerts
	sent := 0
	for _, p := range items {
		daysLeft := int(p.ExpiresAt.Sub(today).Hours() / 24)
		slot := passportSlot(daysLeft)
		err := a.once(ctx, ledgerPassport, p.CustomerID.String(), strconv.Itoa(slot), func(ctx context.Context) error {
			id := p.CustomerID
			sent++
			return a.emitTo(ctx, p.OwnerID, appnotification.EmitInput{
				BranchID: p.BranchID, Kind: notificationdomain.KindPassportExpiring,
				Title: "Passport expiring", Body: passportBody(p.FullName, daysLeft),
				EntityType: "customer", EntityID: &id, HrefHint: "/customers/" + p.CustomerID.String(),
				Meta: map[string]any{"days_left": daysLeft, "booking_id": p.BookingID},
			})
		})
		if err != nil {
			return err
		}
	}
	return j.logCount(ctx, "passport expiry reminders", sent, nil)
}

// passportSlot is the tightest slot daysLeft falls into; expired passports
// share the 0 slot.
func passportSlot(daysLeft int) int {
	if daysLeft < 0 {
		return 0
	}
	slot := PassportReminderDays[0]
	for _, d := range PassportReminderDays {
		if daysLeft <= d {
			slot = d
		}
	}
	return slot
}

func passportBody(name string, daysLeft int) string {
	if daysLeft < 0 {
		return name + "'s passport has expired"
	}
	return fmt.Sprintf("%s's passport expires in %d days", name, daysLeft)
}

// MissingDocs opens (or closes) the missing-document task of confirmed
// bookings past the branch window and tells the owner once a day.
func (j *Jobs) MissingDocs(ctx context.Context, _ []byte) error {
	items, err := j.d.Queries.BookingsAwaitingDocs(ctx, j.now(), sweepBatch)
	if err != nil {
		return err
	}
	a := j.d.Alerts
	flagged := 0
	for _, b := range items {
		cl, err := j.d.Checklist.Checklist(ctx, b.ID)
		if err != nil {
			return err
		}
		if cl == nil || len(cl.MissingRequired) == 0 {
			if _, err := j.d.Tasks.CloseByRule(ctx, taskdomain.RuleMissingDocument, apptask.RelatedBooking, b.ID, "documents complete"); err != nil {
				return err
			}
			continue
		}
		flagged++
		if err := j.d.Tasks.EnsureMissingDocTask(ctx, b.BranchID, b.ID, b.OwnerID, cl.MissingRequired); err != nil {
			return err
		}
		err = a.once(ctx, ledgerMissingDocs, b.ID.String(), a.today(), func(ctx context.Context) error {
			id := b.ID
			return a.emitTo(ctx, b.OwnerID, appnotification.EmitInput{
				BranchID: b.BranchID, Kind: notificationdomain.KindDocumentMissing,
				Title:      "Documents missing",
				Body:       fmt.Sprintf("%s: %s", b.DepartureCode, strings.Join(cl.MissingRequired, ", ")),
				EntityType: "booking", EntityID: &id, HrefHint: "/bookings/" + b.ID.String(),
				Meta: map[string]any{"missing": cl.MissingRequired},
			})
		})
		if err != nil {
			return err
		}
	}
	return j.logCount(ctx, "bookings missing documents", flagged, nil)
}

// PaymentOverdue alerts booking owners about schedules unpaid past the branch
// payment_overdue_hours, once per schedule.
func (j *Jobs) PaymentOverdue(ctx context.Context, _ []byte) error {
	items, err := j.d.Queries.OverdueSchedules(ctx, j.now(), sweepBatch)
	if err != nil {
		return err
	}
	sent := 0
	for _, s := range items {
		ok, err := j.d.Alerts.PaymentOverdue(ctx, s)
		if err != nil {
			return err
		}
		if ok {
			sent++
		}
	}
	return j.logCount(ctx, "payment overdue alerts", sent, nil)
}

// IdleLeads tells owners about open leads with no next action, once a day.
func (j *Jobs) IdleLeads(ctx context.Context, _ []byte) error {
	items, err := j.d.Queries.IdleLeads(ctx, j.now(), sweepBatch)
	if err != nil {
		return err
	}
	a := j.d.Alerts
	sent := 0
	for _, l := range items {
		err := a.once(ctx, ledgerLeadIdle, l.ID.String(), a.today(), func(ctx context.Context) error {
			id := l.ID
			sent++
			return a.emitTo(ctx, l.OwnerID, appnotification.EmitInput{
				BranchID: l.BranchID, Kind: notificationdomain.KindLeadNoFollowUp,
				Title: "Lead has no next action", Body: l.FullName + " has no open follow-up",
				EntityType: "lead", EntityID: &id, HrefHint: "/pipeline",
				Meta: map[string]any{"idle_since": l.IdleSince},
			})
		})
		if err != nil {
			return err
		}
	}
	return j.logCount(ctx, "idle lead alerts", sent, nil)
}

// ReminderPayload is the reminder.send payload: an explicit nudge for a task.
type ReminderPayload struct {
	TaskID uuid.UUID `json:"task_id"`
}

// Reminder notifies the assignee of a still-open task; closed or unassigned
// tasks are acknowledged without effect.
func (j *Jobs) Reminder(ctx context.Context, payload []byte) error {
	var p ReminderPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.TaskID == uuid.Nil {
		return fmt.Errorf("decode reminder payload: invalid task_id")
	}
	t, err := j.d.TaskRead.FindByID(ctx, p.TaskID)
	if errors.Is(err, shared.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if t == nil || (t.Status != taskdomain.StatusOpen && t.Status != taskdomain.StatusInProgress) {
		return nil
	}
	id := t.ID
	return j.d.Alerts.emitTo(ctx, t.AssigneeID, appnotification.EmitInput{
		BranchID: t.BranchID, Kind: notificationdomain.KindTaskReminder, Title: "Task reminder",
		Body: t.Title, EntityType: "task", EntityID: &id, HrefHint: "/tasks",
	})
}
