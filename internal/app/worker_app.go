package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/dataprotection"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/worker"
)

// outboxInterval is how often the dispatcher polls when the outbox is idle.
const outboxInterval = time.Second

// Worker is the background composition root: job handlers, the cron
// scheduler and the outbox dispatcher over the same modules as the API.
type Worker struct {
	mods   *modules
	srv    *worker.Server
	sched  *worker.Scheduler
	log    *slog.Logger
	cancel context.CancelFunc
}

func NewWorker(ctx context.Context, cfg config.Config, log *slog.Logger) (*Worker, error) {
	m, err := buildModules(ctx, cfg, log)
	if err != nil {
		return nil, err
	}
	w, err := newWorker(m)
	if err != nil {
		m.close()
		return nil, err
	}
	return w, nil
}

func newWorker(m *modules) (*Worker, error) {
	cfg, log := m.cfg, m.log
	srv, err := worker.NewServer(cfg.Redis, log)
	if err != nil {
		return nil, fmt.Errorf("worker server: %w", err)
	}
	srv.SetImportProcessor(m.imports.ProcessByStringID)
	srv.SetSecurityCleanup(func(ctx context.Context) error {
		_, err := m.retention.Run(ctx, uuid.Nil)
		return err
	})
	srv.SetEncryptBackfill(func(ctx context.Context, rehash bool) error {
		_, err := m.backfill.Run(ctx, uuid.Nil, dataprotection.Options{Rehash: rehash})
		return err
	})

	registered := map[shared.JobName]bool{}
	register := func(name shared.JobName, h worker.JobHandler) {
		srv.Register(name, h)
		registered[name] = true
	}
	fin := newFinanceJobs(m)
	register(fxdomain.JobRatesSync, fin.SyncRates)
	register(fxdomain.JobLiveSync, fin.SyncLive)
	register(paymentdomain.JobPromisesCheck, fin.CheckPromises)
	register(paymentdomain.JobSchedulesOverdue, fin.MarkSchedulesOverdue)
	register(bookingdomain.JobHoldExpiry, m.lifecycle.HandleHoldExpiry)
	register(bookingdomain.JobTravelledSweep, m.lifecycle.HandleTravelledSweep)
	register(bookingdomain.JobRecomputeSweep, m.lifecycle.HandleRecomputeSweep)
	register(bookingdomain.JobRecompute, m.lifecycle.HandleRecompute)
	for name, h := range m.jobs.Handlers() {
		register(name, worker.JobHandler(h))
	}

	entries, err := buildSchedule(cfg, registered)
	if err != nil {
		return nil, err
	}
	sched, err := worker.NewScheduler(cfg.Redis, businessLocation(cfg), entries, log)
	if err != nil {
		return nil, fmt.Errorf("scheduler: %w", err)
	}
	return &Worker{mods: m, srv: srv, sched: sched, log: log}, nil
}

// Run starts the dispatcher and scheduler and blocks serving jobs.
func (w *Worker) Run() error {
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.mods.dispatcher.Run(ctx, outboxInterval)
	go func() {
		if err := w.sched.Run(); err != nil {
			w.log.Error("scheduler stopped with error", "error", err)
		}
	}()
	return w.srv.Run()
}

func (w *Worker) Shutdown() {
	if w.cancel != nil {
		w.cancel()
	}
	w.sched.Shutdown()
	w.srv.Shutdown()
}

func (w *Worker) Close() { w.mods.close() }
