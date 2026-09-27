// Package outbox writes durable events with the business transaction and
// delivers them to subscribers with retry and dead-lettering (T-281).
package outbox

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/app/realtime"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/outbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

// Writer implements events.Outbox on top of the repository.
type Writer struct {
	repo domain.Repository
}

func NewWriter(repo domain.Repository) *Writer { return &Writer{repo: repo} }

func (w *Writer) Add(ctx context.Context, ev events.Event) error {
	raw, branch, err := events.Encode(ev)
	if err != nil {
		return err
	}
	return w.repo.Insert(ctx, ev.Name, raw, branch)
}

// Deliverer runs every subscriber of an event and reports their failures.
type Deliverer interface {
	Deliver(ctx context.Context, ev events.Event) error
}

type Options struct {
	Batch int
	Lease time.Duration
	// Retention keeps dispatched rows this long for diagnosis before purge.
	Retention time.Duration
	Now       func() time.Time
	Log       *slog.Logger
}

// Dispatcher claims due records, delivers them and records the outcome.
type Dispatcher struct {
	repo      domain.Repository
	bus       Deliverer
	announcer realtime.Announcer
	opt       Options
}

func NewDispatcher(repo domain.Repository, bus Deliverer, announcer realtime.Announcer, opt Options) *Dispatcher {
	if opt.Batch <= 0 {
		opt.Batch = 50
	}
	if opt.Lease <= 0 {
		opt.Lease = 2 * time.Minute
	}
	if opt.Retention <= 0 {
		opt.Retention = 7 * 24 * time.Hour
	}
	if opt.Now == nil {
		opt.Now = func() time.Time { return time.Now().UTC() }
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	return &Dispatcher{repo: repo, bus: bus, announcer: announcer, opt: opt}
}

// DispatchOnce delivers one batch and returns how many records it handled.
func (d *Dispatcher) DispatchOnce(ctx context.Context) (int, error) {
	recs, err := d.repo.Claim(ctx, d.opt.Now(), d.opt.Lease, d.opt.Batch)
	if err != nil {
		return 0, err
	}
	for i := range recs {
		d.deliver(ctx, &recs[i])
	}
	return len(recs), nil
}

func (d *Dispatcher) deliver(ctx context.Context, rec *domain.Record) {
	ev, err := events.Decode(rec.Name, rec.Payload)
	if err == nil {
		err = d.bus.Deliver(ctx, ev)
	}
	now := d.opt.Now()
	if err != nil {
		next, dead := domain.AfterFailure(rec.Attempts, now)
		if dead {
			d.opt.Log.ErrorContext(ctx, "outbox event dead-lettered", "id", rec.ID, "event", rec.Name, "attempts", rec.Attempts, "error", err)
		} else {
			d.opt.Log.WarnContext(ctx, "outbox delivery failed", "id", rec.ID, "event", rec.Name, "attempts", rec.Attempts, "retry_at", next, "error", err)
		}
		if mErr := d.repo.MarkFailed(ctx, rec.ID, next, dead, err.Error()); mErr != nil {
			d.opt.Log.ErrorContext(ctx, "outbox mark failed", "id", rec.ID, "error", mErr)
		}
		return
	}
	if mErr := d.repo.MarkDispatched(ctx, rec.ID, now); mErr != nil {
		d.opt.Log.ErrorContext(ctx, "outbox mark dispatched", "id", rec.ID, "error", mErr)
		return
	}
	if d.announcer != nil && rec.BranchID != nil {
		_ = d.announcer.Announce(ctx, realtime.Signal{Type: realtime.TypeInvalidate, BranchID: rec.BranchID, Topic: rec.Name})
	}
}

// Run drains the outbox every interval until ctx ends; a full batch is
// followed immediately by the next one.
func (d *Dispatcher) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		n, err := d.DispatchOnce(ctx)
		if err != nil && ctx.Err() == nil {
			d.opt.Log.ErrorContext(ctx, "outbox claim failed", "error", err)
		}
		wait := interval
		if n == d.opt.Batch {
			wait = 0
		}
		timer.Reset(wait)
	}
}

// Purge deletes dispatched rows older than the retention window.
func (d *Dispatcher) Purge(ctx context.Context) (int64, error) {
	return d.repo.PurgeDispatched(ctx, d.opt.Now().Add(-d.opt.Retention))
}

// Admin exposes outbox health and dead-letter recovery to ops.
type Admin struct {
	repo domain.Repository
}

func NewAdmin(repo domain.Repository) *Admin { return &Admin{repo: repo} }

func (a *Admin) Stats(ctx context.Context) (domain.Stats, error) { return a.repo.Stats(ctx) }

func (a *Admin) Dead(ctx context.Context, limit int) ([]domain.Record, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return a.repo.ListDead(ctx, limit)
}

func (a *Admin) Requeue(ctx context.Context, id uuid.UUID) error {
	err := a.repo.Requeue(ctx, id)
	if errors.Is(err, domain.ErrNotFound) {
		return shared.NewNotFound("dead outbox event")
	}
	return err
}
