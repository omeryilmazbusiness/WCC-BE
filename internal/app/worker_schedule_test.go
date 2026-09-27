package app

import (
	"strings"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/app/automation"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/worker"
)

func allRegistered(cfg config.Config) map[shared.JobName]bool {
	out := map[shared.JobName]bool{}
	for _, e := range defaultSchedule(cfg) {
		out[e.Job] = true
	}
	return out
}

func specOf(entries []worker.ScheduleEntry, job shared.JobName) (string, bool) {
	for _, e := range entries {
		if e.Job == job {
			return e.Spec, true
		}
	}
	return "", false
}

func TestScheduleCoversEpic22Table(t *testing.T) {
	cfg := config.Config{}
	entries, err := buildSchedule(cfg, allRegistered(cfg))
	if err != nil {
		t.Fatal(err)
	}
	want := map[shared.JobName]string{
		shared.JobSLASweep:            "* * * * *",
		automation.JobEscalations:     "*/5 * * * *",
		automation.JobPaymentDue:      "0 * * * *",
		automation.JobPaymentOverdue:  "35 * * * *",
		automation.JobDocumentExpiry:  "0 6 * * *",
		automation.JobPassportExpiry:  "15 6 * * *",
		automation.JobSupplierConfirm: "30 6 * * *",
		shared.JobAISummaryDaily:      "5 * * * *",
		automation.JobTargetRecompute: "20 * * * *",
		shared.JobReportGenerate:      "30 * * * *",
	}
	for job, spec := range want {
		if got, ok := specOf(entries, job); !ok || got != spec {
			t.Fatalf("%s spec = %q (present %v), want %q", job, got, ok, spec)
		}
	}
}

func TestScheduleOverrides(t *testing.T) {
	cfg := config.Config{Redis: config.RedisConfig{ScheduleOverrides: map[string]string{
		config.ScheduleKey(string(shared.JobSLASweep)):           "*/2 * * * *",
		config.ScheduleKey(string(automation.JobPassportExpiry)): "OFF",
	}}}
	entries, err := buildSchedule(cfg, allRegistered(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if spec, _ := specOf(entries, shared.JobSLASweep); spec != "*/2 * * * *" {
		t.Fatalf("sla spec = %q", spec)
	}
	if _, ok := specOf(entries, automation.JobPassportExpiry); ok {
		t.Fatal("disabled job still scheduled")
	}
}

func TestScheduleRejectsTyposAndUnregisteredJobs(t *testing.T) {
	cfg := config.Config{Redis: config.RedisConfig{ScheduleOverrides: map[string]string{"INBOX_SLA_SWEPE": "off"}}}
	if _, err := buildSchedule(cfg, allRegistered(cfg)); err == nil || !strings.Contains(err.Error(), "SCHEDULE_INBOX_SLA_SWEPE") {
		t.Fatalf("typo err = %v", err)
	}
	reg := allRegistered(config.Config{})
	delete(reg, automation.JobMissingDocs)
	if _, err := buildSchedule(config.Config{}, reg); err == nil {
		t.Fatal("expected error for a scheduled job without handler")
	}
}
