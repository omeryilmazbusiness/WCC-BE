package worker

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/hibiken/asynq"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// ScheduleEntry enqueues Job on the cron Spec (5-field, scheduler time zone).
type ScheduleEntry struct {
	Spec    string
	Job     shared.JobName
	Payload []byte
	Queue   string
}

// Validate rejects entries the scheduler would silently drop.
func (e ScheduleEntry) Validate() error {
	if e.Spec == "" || e.Job == "" {
		return fmt.Errorf("schedule entry needs spec and job (got %q, %q)", e.Spec, e.Job)
	}
	return nil
}

// Scheduler enqueues periodic jobs. Every worker replica may run one: each
// tick is enqueued with a uniqueness lock, so replicas never double-fire.
type Scheduler struct {
	log       *slog.Logger
	scheduler *asynq.Scheduler
}

// uniqueFor bounds the dedupe lock; it must stay below the shortest interval.
const uniqueFor = 50 * time.Second

func NewScheduler(cfg config.RedisConfig, loc *time.Location, entries []ScheduleEntry, log *slog.Logger) (*Scheduler, error) {
	if cfg.URL == "" || cfg.URL == "memory://" {
		return nil, fmt.Errorf("scheduler requires redis URL (got memory/empty)")
	}
	opt, err := asynq.ParseRedisURI(cfg.URL)
	if err != nil {
		return nil, err
	}
	if loc == nil {
		loc = time.UTC
	}
	s := asynq.NewScheduler(opt, &asynq.SchedulerOpts{
		Location: loc,
		EnqueueErrorHandler: func(t *asynq.Task, _ []asynq.Option, err error) {
			log.Error("scheduled enqueue failed", "type", t.Type(), "error", err)
		},
	})
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			return nil, err
		}
		queue := e.Queue
		if queue == "" {
			queue = "default"
		}
		task := asynq.NewTask(string(e.Job), e.Payload)
		if _, err := s.Register(e.Spec, task, asynq.Queue(queue), asynq.Unique(uniqueFor)); err != nil {
			return nil, fmt.Errorf("schedule %s (%s): %w", e.Job, e.Spec, err)
		}
	}
	return &Scheduler{log: log, scheduler: s}, nil
}

func (s *Scheduler) Run() error {
	s.log.Info("asynq scheduler started")
	return s.scheduler.Run()
}

func (s *Scheduler) Shutdown() {
	s.scheduler.Shutdown()
}
