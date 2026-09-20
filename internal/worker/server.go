package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Server wraps Asynq worker with retry + archived (DLQ) visibility.
type Server struct {
	log    *slog.Logger
	server *asynq.Server
	mux    *asynq.ServeMux
}

func NewServer(cfg config.RedisConfig, log *slog.Logger) (*Server, error) {
	if cfg.URL == "" || cfg.URL == "memory://" {
		return nil, fmt.Errorf("worker requires redis URL (got memory/empty)")
	}
	opt, err := asynq.ParseRedisURI(cfg.URL)
	if err != nil {
		return nil, err
	}
	srv := asynq.NewServer(opt, asynq.Config{
		Concurrency: 10,
		Queues: map[string]int{
			"critical": 6,
			"default":  3,
			"low":      1,
		},
		RetryDelayFunc: asynq.DefaultRetryDelayFunc,
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			retried, _ := asynq.GetRetryCount(ctx)
			maxRetry, _ := asynq.GetMaxRetry(ctx)
			log.Error("job failed",
				"type", task.Type(),
				"retried", retried,
				"max_retry", maxRetry,
				"error", err,
			)
		}),
	})
	mux := asynq.NewServeMux()
	s := &Server{log: log, server: srv, mux: mux}
	s.registerHandlers()
	return s, nil
}

func (s *Server) registerHandlers() {
	s.mux.HandleFunc(string(shared.JobReminderSend), s.handleReminder)
	s.mux.HandleFunc(string(shared.JobWebhookRetry), s.handleWebhookRetry)
	s.mux.HandleFunc(string(shared.JobImportProcess), s.handleImport)
	s.mux.HandleFunc(string(shared.JobReportGenerate), s.handleReport)
	s.mux.HandleFunc(string(shared.JobAISummaryDaily), s.handleAISummary)
}

func (s *Server) Run() error {
	s.log.Info("asynq worker listening")
	return s.server.Run(s.mux)
}

func (s *Server) Shutdown() {
	s.server.Shutdown()
}

type ReminderPayload struct {
	TaskID string `json:"task_id"`
}

func (s *Server) handleReminder(ctx context.Context, t *asynq.Task) error {
	var p ReminderPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode reminder: %w", err)
	}
	s.log.Info("processed reminder job", "task_id", p.TaskID)
	return nil
}

type WebhookRetryPayload struct {
	Provider  string `json:"provider"`
	EventID   string `json:"event_id"`
	Attempt   int    `json:"attempt"`
}

func (s *Server) handleWebhookRetry(ctx context.Context, t *asynq.Task) error {
	var p WebhookRetryPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode webhook retry: %w", err)
	}
	s.log.Info("processed webhook retry job", "provider", p.Provider, "event_id", p.EventID)
	return nil
}

type ImportPayload struct {
	ImportJobID string `json:"import_job_id"`
}

func (s *Server) handleImport(ctx context.Context, t *asynq.Task) error {
	var p ImportPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode import: %w", err)
	}
	s.log.Info("processed import job", "import_job_id", p.ImportJobID)
	return nil
}

func (s *Server) handleReport(ctx context.Context, t *asynq.Task) error {
	s.log.Info("processed report job", "bytes", len(t.Payload()))
	return nil
}

func (s *Server) handleAISummary(ctx context.Context, t *asynq.Task) error {
	s.log.Info("processed ai summary job", "bytes", len(t.Payload()))
	return nil
}
