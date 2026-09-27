package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/hibiken/asynq"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ProcessFunc processes an import job by ID (wired from app service).
type ProcessFunc func(ctx context.Context, importJobID string) error

// CleanupFunc runs the idempotent security retention purge.
type CleanupFunc func(ctx context.Context) error

// EncryptBackfillFunc runs the idempotent encrypt backfill.
type EncryptBackfillFunc func(ctx context.Context, rehash bool) error

// Server wraps Asynq worker with retry + archived (DLQ) visibility.
type Server struct {
	log             *slog.Logger
	server          *asynq.Server
	mux             *asynq.ServeMux
	importProcess   ProcessFunc
	securityCleanup CleanupFunc
	encryptBackfill EncryptBackfillFunc
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

// SetImportProcessor wires the idempotent import Process handler (DIP).
func (s *Server) SetImportProcessor(fn ProcessFunc) {
	s.importProcess = fn
}

// SetSecurityCleanup wires the security.cleanup job handler.
func (s *Server) SetSecurityCleanup(fn CleanupFunc) {
	s.securityCleanup = fn
}

// SetEncryptBackfill wires the security.encrypt_backfill job handler.
func (s *Server) SetEncryptBackfill(fn EncryptBackfillFunc) {
	s.encryptBackfill = fn
}

// systemScope runs every job with the system access scope: jobs carry no
// caller, and branch-bound work narrows the scope itself.
func systemScope(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		return next.ProcessTask(audit.AsSystem(access.WithScope(ctx, access.System())), t)
	})
}

func (s *Server) registerHandlers() {
	s.mux.Use(systemScope)
	s.mux.HandleFunc(string(shared.JobImportProcess), s.handleImport)
	s.mux.HandleFunc(string(shared.JobSecurityCleanup), s.handleSecurityCleanup)
	s.mux.HandleFunc(string(shared.JobEncryptBackfill), s.handleEncryptBackfill)
}

func (s *Server) Run() error {
	s.log.Info("asynq worker listening")
	return s.server.Run(s.mux)
}

func (s *Server) Shutdown() {
	s.server.Shutdown()
}

type ImportPayload struct {
	ImportJobID string `json:"import_job_id"`
}

func (s *Server) handleImport(ctx context.Context, t *asynq.Task) error {
	var p ImportPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode import: %w", err)
	}
	if s.importProcess != nil {
		if err := s.importProcess(ctx, p.ImportJobID); err != nil {
			return err
		}
	}
	s.log.Info("processed import job", "import_job_id", p.ImportJobID)
	return nil
}

func (s *Server) handleSecurityCleanup(ctx context.Context, _ *asynq.Task) error {
	if s.securityCleanup == nil {
		return fmt.Errorf("security cleanup not wired")
	}
	if err := s.securityCleanup(ctx); err != nil {
		return err
	}
	s.log.Info("processed security cleanup job")
	return nil
}

type EncryptBackfillPayload struct {
	Rehash bool `json:"rehash"`
}

func (s *Server) handleEncryptBackfill(ctx context.Context, t *asynq.Task) error {
	if s.encryptBackfill == nil {
		return fmt.Errorf("encrypt backfill not wired")
	}
	var p EncryptBackfillPayload
	if len(t.Payload()) > 0 {
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("decode encrypt backfill: %w", err)
		}
	}
	if err := s.encryptBackfill(ctx, p.Rehash); err != nil {
		return err
	}
	s.log.Info("processed encrypt backfill job", "rehash", p.Rehash)
	return nil
}
