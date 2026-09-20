package worker

// Package worker hosts Asynq task handlers (reminders, webhook retry, import).
// Handlers must be idempotent; enqueue from application services after commit.
