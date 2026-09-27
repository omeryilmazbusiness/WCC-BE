package worker

import (
	"context"

	"github.com/hibiken/asynq"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// JobHandler processes one job payload. Jobs register through Register so
// the server stays closed for modification when new jobs are added.
type JobHandler func(ctx context.Context, payload []byte) error

// Register binds a job type to its handler. Handlers run with the system
// access scope like every other job.
func (s *Server) Register(name shared.JobName, h JobHandler) {
	s.mux.HandleFunc(string(name), func(ctx context.Context, t *asynq.Task) error {
		return h(ctx, t.Payload())
	})
}
