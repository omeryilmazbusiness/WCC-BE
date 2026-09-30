package app

import (
	"fmt"
	"strings"

	"github.com/wodi-crm/wodi-crm-be/internal/app/automation"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/worker"
)

// scheduleOff disables a job through SCHEDULE_<JOB>=off.
const scheduleOff = "off"

// defaultSchedule is the cron table (T-279), evaluated in SCHEDULER_TZ.
// Branch-local work (the 07:00 AI summary) runs hourly and checks each
// branch's own clock.
func defaultSchedule(cfg config.Config) []worker.ScheduleEntry {
	entries := []worker.ScheduleEntry{
		{Spec: "* * * * *", Job: shared.JobSLASweep, Queue: "critical"},
		{Spec: "*/5 * * * *", Job: automation.JobEscalations, Queue: "critical"},
		{Spec: "*/5 * * * *", Job: taskdomain.JobOverdueSweep},
		{Spec: "*/15 * * * *", Job: taskdomain.JobEscalateOverdue},
		{Spec: "*/5 * * * *", Job: bookingdomain.JobHoldExpiry},
		{Spec: "*/2 * * * *", Job: shared.JobWebhookRetry},
		{Spec: "0 * * * *", Job: automation.JobPaymentDue},
		{Spec: "35 * * * *", Job: automation.JobPaymentOverdue},
		{Spec: "10 * * * *", Job: paymentdomain.JobSchedulesOverdue},
		{Spec: "20 * * * *", Job: automation.JobTargetRecompute},
		{Spec: "25 * * * *", Job: automation.JobIdleLeads},
		{Spec: "30 * * * *", Job: shared.JobReportGenerate, Queue: "low"},
		{Spec: "45 * * * *", Job: bookingdomain.JobRecomputeSweep},
		{Spec: "50 * * * *", Job: automation.JobMissingDocs},
		{Spec: "5 * * * *", Job: shared.JobAISummaryDaily, Queue: "low"},
		{Spec: "40 * * * *", Job: automation.JobLostLeadsWeekly, Queue: "low"},
		{Spec: "15 0 * * *", Job: bookingdomain.JobTravelledSweep},
		{Spec: "30 3 * * *", Job: automation.JobOutboxPurge, Queue: "low"},
		{Spec: "0 6 * * *", Job: automation.JobDocumentExpiry},
		{Spec: "15 6 * * *", Job: automation.JobPassportExpiry},
		{Spec: "30 6 * * *", Job: automation.JobSupplierConfirm},
		{Spec: "45 6 * * *", Job: automation.JobVisaFollowUp},
		{Spec: "0 8 * * *", Job: paymentdomain.JobPromisesCheck},
	}
	if cfg.FX.ProviderURL != "" || cfg.FX.AccountingEnabled() {
		entries = append(entries, worker.ScheduleEntry{Spec: "30 6 * * *", Job: fxdomain.JobRatesSync})
	}
	if cfg.FX.LiveEnabled {
		entries = append(entries, worker.ScheduleEntry{Spec: "*/10 * * * *", Job: fxdomain.JobLiveSync})
	}
	return entries
}

// buildSchedule applies SCHEDULE_<JOB> overrides to the defaults and checks
// that every scheduled job has a handler; an unknown override key fails too,
// so a typo never silently leaves the default in place.
func buildSchedule(cfg config.Config, registered map[shared.JobName]bool) ([]worker.ScheduleEntry, error) {
	defaults := defaultSchedule(cfg)
	known := make(map[string]bool, len(defaults))
	out := make([]worker.ScheduleEntry, 0, len(defaults))
	for _, e := range defaults {
		key := config.ScheduleKey(string(e.Job))
		known[key] = true
		if spec, ok := cfg.Redis.ScheduleOverrides[key]; ok {
			if strings.EqualFold(spec, scheduleOff) {
				continue
			}
			e.Spec = spec
		}
		if !registered[e.Job] {
			return nil, fmt.Errorf("scheduled job %s has no handler", e.Job)
		}
		out = append(out, e)
	}
	for key := range cfg.Redis.ScheduleOverrides {
		if !known[key] {
			return nil, fmt.Errorf("SCHEDULE_%s does not match a scheduled job", key)
		}
	}
	return out, nil
}
