package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	appdocument "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	notificationdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	reportdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/report"
	targetdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	supplierdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	visadomain "github.com/wodi-crm/wodi-crm-be/internal/domain/visa"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Ledger job names for once-per-period alerts.
const (
	ledgerTargetBehind = "alert.target_behind"
	ledgerPaymentDue   = "alert.payment_due"
	ledgerSupplierLow  = "alert.supplier_low_balance"
	ledgerSupplierEnd  = "alert.supplier_contract"
	ledgerPaymentLate  = "alert.payment_overdue"
	ledgerLeadIdle     = "alert.lead_idle"
	ledgerPassport     = "alert.passport_expiry"
	ledgerMissingDocs  = "alert.missing_docs"
	ledgerAISummary    = "ai.summary_daily"
	ledgerLostLeads    = "ai.lost_leads_weekly"
)

// Alerts implements the alert ports of the document, supplier, visa, target,
// payment and report modules (DIP): each module decides when, Alerts decides
// who hears about it and which rule task follows.
type Alerts struct {
	notify Notifier
	tasks  RuleTasks
	staff  Staff
	ledger shared.RunLedger
	tx     tx.Runner
	now    func() time.Time
}

func NewAlerts(notify Notifier, tasks RuleTasks, staff Staff, ledger shared.RunLedger, runner tx.Runner) *Alerts {
	return &Alerts{notify: notify, tasks: tasks, staff: staff, ledger: ledger, tx: runner,
		now: func() time.Time { return time.Now().UTC() }}
}

// once runs fn at most once per (job, key, period); a failing fn releases the slot.
func (a *Alerts) once(ctx context.Context, job, key, period string, fn func(context.Context) error) error {
	if a.ledger == nil {
		return fn(ctx)
	}
	return a.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		claimed, err := a.ledger.Claim(ctx, job, key, period)
		if err != nil || !claimed {
			return err
		}
		return fn(ctx)
	})
}

func (a *Alerts) today() string { return a.now().Format(time.DateOnly) }

// firstStaff picks the assignee for role-owned work; uuid.Nil when nobody holds the roles.
func (a *Alerts) firstStaff(ctx context.Context, branchID uuid.UUID, roles []string) (uuid.UUID, error) {
	if a.staff == nil {
		return uuid.Nil, nil
	}
	users, err := a.staff.ListActiveByRoles(ctx, branchID, roles)
	if err != nil || len(users) == 0 {
		return uuid.Nil, err
	}
	return users[0].ID, nil
}

func (a *Alerts) emitTo(ctx context.Context, userID uuid.UUID, in appnotification.EmitInput) error {
	if userID == uuid.Nil {
		return nil
	}
	in.RecipientUserID = userID
	_, err := a.notify.Emit(ctx, in)
	return err
}

func documentHref(doc appdocument.DocumentDTO) string {
	switch doc.RelatedType {
	case "booking":
		return "/bookings/" + doc.RelatedID.String()
	case "customer":
		return "/customers/" + doc.RelatedID.String()
	default:
		return "/documents"
	}
}

// DocumentExpiring implements document.ExpiryAlerts.
func (a *Alerts) DocumentExpiring(ctx context.Context, doc appdocument.DocumentDTO, daysLeft int) error {
	id := doc.ID
	return a.emitTo(ctx, doc.UploadedBy, appnotification.EmitInput{
		BranchID: doc.BranchID, Kind: notificationdomain.KindDocumentExpiring,
		Title:      "Document expiring",
		Body:       fmt.Sprintf("%s (%s) expires in %d days", doc.FileName, doc.Kind, daysLeft),
		EntityType: "document", EntityID: &id, HrefHint: documentHref(doc),
		Meta: map[string]any{"days_left": daysLeft},
	})
}

// DocumentExpired implements document.ExpiryAlerts.
func (a *Alerts) DocumentExpired(ctx context.Context, doc appdocument.DocumentDTO) error {
	id := doc.ID
	return a.emitTo(ctx, doc.UploadedBy, appnotification.EmitInput{
		BranchID: doc.BranchID, Kind: notificationdomain.KindDocumentExpiring,
		Title:      "Document expired",
		Body:       fmt.Sprintf("%s (%s) has expired and needs a replacement", doc.FileName, doc.Kind),
		EntityType: "document", EntityID: &id, HrefHint: documentHref(doc),
		Severity: notificationdomain.SeverityCritical,
	})
}

// SupplierUnconfirmed implements supplier.ConfirmationFollowUp.
func (a *Alerts) SupplierUnconfirmed(ctx context.Context, sup *supplierdomain.Supplier, link *supplierdomain.Link) error {
	if sup == nil || link == nil {
		return nil
	}
	label := strings.TrimSpace(sup.NameEn + " · " + string(link.LinkType))
	assignee, err := a.firstStaff(ctx, sup.BranchID, operationsRoles)
	if err != nil {
		return err
	}
	if assignee != uuid.Nil {
		if err := a.tasks.EnsureSupplierConfirmTask(ctx, sup.BranchID, link.ID, assignee, label); err != nil {
			return err
		}
	}
	id := link.ID
	_, err = a.notify.EmitToRoles(ctx, sup.BranchID, operationsRoles, appnotification.EmitInput{
		Kind: notificationdomain.KindSupplierUnconfirmed, Title: "Supplier not confirmed",
		Body: label, EntityType: "supplier_link", EntityID: &id, HrefHint: "/suppliers",
	})
	return err
}

// SupplierConfirmed implements supplier.ConfirmationFollowUp.
func (a *Alerts) SupplierConfirmed(ctx context.Context, link *supplierdomain.Link) error {
	if link == nil {
		return nil
	}
	_, err := a.tasks.CloseByRule(ctx, taskdomain.RuleSupplierConfirm, apptask.RelatedSupplierLink, link.ID, "confirmed")
	return err
}

// SupplierLowBalance implements supplier.FundsAlerts; at most once a day per
// supplier, critical once the account is exhausted (closed to search).
func (a *Alerts) SupplierLowBalance(ctx context.Context, sup *supplierdomain.Supplier) error {
	if sup == nil {
		return nil
	}
	key := sup.ID.String()
	if sup.Finance.Exhausted() {
		key += ":exhausted"
	}
	return a.once(ctx, ledgerSupplierLow, key, a.today(), func(ctx context.Context) error {
		avail, _ := sup.Finance.Available()
		id := sup.ID
		in := appnotification.EmitInput{
			Kind: notificationdomain.KindSupplierLowBalance, Title: "Supplier balance low: " + supplierLabel(sup),
			Body:       fmt.Sprintf("%s available, threshold %s", shared.FormatMinor(avail, sup.Finance.Currency), shared.FormatMinor(sup.Finance.LowBalanceThreshold, sup.Finance.Currency)),
			EntityType: "supplier", EntityID: &id, HrefHint: "/suppliers/" + id.String() + "?tab=finance",
		}
		if sup.Finance.Exhausted() {
			in.Title = "Supplier closed to search: " + supplierLabel(sup)
			in.Body = "Deposit or credit line exhausted; top up or settle to reopen"
			in.Severity = notificationdomain.SeverityCritical
		}
		_, err := a.notify.EmitToRoles(ctx, sup.BranchID, financeRoles, in)
		return err
	})
}

// SupplierContractExpiring implements supplier.FundsAlerts.
func (a *Alerts) SupplierContractExpiring(ctx context.Context, sup *supplierdomain.Supplier, daysLeft int) error {
	if sup == nil || sup.ContractEnd == nil {
		return nil
	}
	return a.once(ctx, ledgerSupplierEnd, sup.ID.String(), a.today(), func(ctx context.Context) error {
		id := sup.ID
		_, err := a.notify.EmitToRoles(ctx, sup.BranchID, operationsRoles, appnotification.EmitInput{
			Kind: notificationdomain.KindSupplierContract, Title: "Supplier contract ending: " + supplierLabel(sup),
			Body:       fmt.Sprintf("Ends on %s (%d days left)", sup.ContractEnd.UTC().Format(time.DateOnly), daysLeft),
			EntityType: "supplier", EntityID: &id, HrefHint: "/suppliers/" + id.String() + "?tab=scope",
		})
		return err
	})
}

func supplierLabel(sup *supplierdomain.Supplier) string {
	if sup.NameEn != "" {
		return sup.NameEn
	}
	if sup.NameAr != "" {
		return sup.NameAr
	}
	return sup.Code
}

// VisaAwaitingDecision implements visa.FollowUps.
func (a *Alerts) VisaAwaitingDecision(ctx context.Context, c visadomain.FollowUpCandidate) error {
	v := c.Case
	ref := v.ExternalRef
	if ref == "" {
		ref = v.ID.String()[:8]
	}
	if c.OwnerID == uuid.Nil {
		return nil
	}
	if err := a.tasks.EnsureVisaFollowUpTask(ctx, v.BranchID, v.ID, c.OwnerID, ref); err != nil {
		return err
	}
	id := v.ID
	return a.emitTo(ctx, c.OwnerID, appnotification.EmitInput{
		BranchID: v.BranchID, Kind: notificationdomain.KindVisaFollowUp,
		Title: "Visa awaiting decision", Body: "Follow up on visa application " + ref,
		EntityType: "visa_application", EntityID: &id, HrefHint: "/bookings/" + v.BookingID.String(),
	})
}

// VisaDecided implements visa.FollowUps.
func (a *Alerts) VisaDecided(ctx context.Context, v *visadomain.VisaCase) error {
	if v == nil {
		return nil
	}
	_, err := a.tasks.CloseByRule(ctx, taskdomain.RuleVisaFollowUp, apptask.RelatedVisaApplication, v.ID, string(v.Status))
	return err
}

// TargetBehind implements revenuetarget.BehindAlerts: managers hear about a
// behind target at most once a day however often the sweep runs.
func (a *Alerts) TargetBehind(ctx context.Context, branchID uuid.UUID, p targetdomain.Progress, deficit int64) error {
	return a.once(ctx, ledgerTargetBehind, p.TargetID.String(), a.today(), func(ctx context.Context) error {
		return a.notifyTargetBehind(ctx, branchID, p.TargetID, p.Label, deficit)
	})
}

func (a *Alerts) notifyTargetBehind(ctx context.Context, branchID, targetID uuid.UUID, label string, deficit int64) error {
	id := targetID
	_, err := a.notify.EmitToRoles(ctx, branchID, managerRoles, appnotification.EmitInput{
		Kind: notificationdomain.KindTargetBehind, Title: "Target behind pace",
		Body:       fmt.Sprintf("%s is %d behind the expected pace", label, deficit),
		EntityType: "revenue_target", EntityID: &id, HrefHint: "/targets",
		Meta: map[string]any{"deficit": deficit},
	})
	return err
}

// PaymentDue implements payment.PaymentDueAlerts (once per booking and due date).
func (a *Alerts) PaymentDue(ctx context.Context, branchID, bookingID, ownerID uuid.UUID, dueAt time.Time, amount int64, currency string) error {
	return a.once(ctx, ledgerPaymentDue, bookingID.String(), dueAt.UTC().Format(time.DateOnly), func(ctx context.Context) error {
		id := bookingID
		return a.emitTo(ctx, ownerID, appnotification.EmitInput{
			BranchID: branchID, Kind: notificationdomain.KindPaymentDue, Title: "Payment due",
			Body:       fmt.Sprintf("%s due on %s", shared.FormatMinor(amount, currency), dueAt.UTC().Format(time.DateOnly)),
			EntityType: "booking", EntityID: &id, HrefHint: "/bookings/" + bookingID.String(),
			Meta: map[string]any{"amount": amount, "currency": currency, "due_at": dueAt.UTC()},
		})
	})
}

// PaymentOverdue tells the booking owner once per schedule that a payment is
// past due beyond the branch grace.
func (a *Alerts) PaymentOverdue(ctx context.Context, s OverdueScheduleRef) (bool, error) {
	sent := false
	err := a.once(ctx, ledgerPaymentLate, s.ScheduleID.String(), "", func(ctx context.Context) error {
		sent = true
		id := s.BookingID
		return a.emitTo(ctx, s.OwnerID, appnotification.EmitInput{
			BranchID: s.BranchID, Kind: notificationdomain.KindPaymentOverdue, Title: "Payment overdue",
			Body:       fmt.Sprintf("%s was due on %s", shared.FormatMinor(s.Amount, s.Currency), s.DueAt.UTC().Format(time.DateOnly)),
			EntityType: "booking", EntityID: &id, HrefHint: "/bookings/" + s.BookingID.String(),
			Meta: map[string]any{"schedule_id": s.ScheduleID, "amount": s.Amount, "currency": s.Currency, "due_at": s.DueAt.UTC()},
		})
	})
	return sent && err == nil, err
}

// ReportReady implements report.RunNotifier.
func (a *Alerts) ReportReady(ctx context.Context, run reportdomain.ScheduledRun, recipients []uuid.UUID) error {
	id := run.ID
	for _, uid := range recipients {
		if err := a.emitTo(ctx, uid, appnotification.EmitInput{
			BranchID: run.BranchID, Kind: notificationdomain.KindReportReady,
			Title:      "Scheduled report ready",
			Body:       fmt.Sprintf("%s report for %s (%d rows)", run.Kind, run.Period, run.RowCount),
			EntityType: "report_run", EntityID: &id, HrefHint: "/reports?run=" + run.ID.String(),
		}); err != nil {
			return err
		}
	}
	return nil
}
