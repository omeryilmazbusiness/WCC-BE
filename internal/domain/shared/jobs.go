package shared

import (
	"context"
	"time"
)

// JobName is a stable queue task type (provider-agnostic).
type JobName string

const (
	JobReminderSend       JobName = "reminder.send"
	JobWebhookRetry       JobName = "integration.webhook_retry"
	JobImportProcess      JobName = "import.process"
	JobReportGenerate     JobName = "report.generate"
	JobAISummaryDaily     JobName = "ai.summary.daily"
	JobSLASweep           JobName = "inbox.sla_sweep"
)

// EnqueueOpts controls retry / delay / uniqueness at the port level.
type EnqueueOpts struct {
	Queue     string        // default | critical | low
	MaxRetry  int           // 0 = adapter default
	ProcessIn time.Duration // delay before first attempt
	UniqueKey string        // idempotency / dedupe key
}

// Enqueuer is the application-facing queue port (ISP / DIP).
type Enqueuer interface {
	Enqueue(ctx context.Context, name JobName, payload []byte, opts EnqueueOpts) (jobID string, err error)
	Ping(ctx context.Context) error
	Close() error
}

// QueueInspector exposes operational visibility (retry + DLQ/archived).
type QueueInspector interface {
	Stats(ctx context.Context) (QueueStats, error)
	GetJob(ctx context.Context, queue, jobID string) (JobInfo, error)
}

// QueueStats aggregates Asynq-style queues for health / ops.
type QueueStats struct {
	Pending   int `json:"pending"`
	Active    int `json:"active"`
	Scheduled int `json:"scheduled"`
	Retry     int `json:"retry"`
	Archived  int `json:"archived"` // dead-letter after max retry
	Completed int `json:"completed"`
	Mode      string `json:"mode"` // redis | memory
}

// JobInfo is a normalized job status view.
type JobInfo struct {
	ID       string    `json:"id"`
	Type     string    `json:"type"`
	Queue    string    `json:"queue"`
	State    string    `json:"state"` // pending|active|retry|archived|completed|scheduled
	Retried  int       `json:"retried"`
	MaxRetry int       `json:"max_retry"`
	LastErr  string    `json:"last_error,omitempty"`
	NextAt   time.Time `json:"next_process_at,omitempty"`
}
