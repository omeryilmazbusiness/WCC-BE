package worker

// Package worker will host Asynq task handlers (reminders, webhook retry, import).
// Keep handlers idempotent; enqueue from application services after commit.
type ReminderPayload struct {
	TaskID string `json:"task_id"`
}
