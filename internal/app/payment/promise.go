package payment

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// PromiseTasks is the task port of payment promises (ISP). Implementations
// are idempotent per promise.
type PromiseTasks interface {
	EnsurePromiseFollowUp(ctx context.Context, p *domain.Promise, assigneeID uuid.UUID, dueAt time.Time) (uuid.UUID, error)
	EnsurePromiseBroken(ctx context.Context, p *domain.Promise, assigneeID uuid.UUID) error
	// CloseTask completes (done) or cancels an open task; closed tasks are left alone.
	CloseTask(ctx context.Context, taskID uuid.UUID, done bool, outcome string) error
}

// PromiseNotifier tells the booking owner a promise was broken.
type PromiseNotifier interface {
	NotifyPromiseBroken(ctx context.Context, p *domain.Promise, ownerID uuid.UUID) error
}

type PromiseDeps struct {
	Tasks    PromiseTasks
	Notifier PromiseNotifier
	Clock    Clock
	Log      *slog.Logger
}

// Promises manages payment promises (T-274). Status on read is the
// effective one (kept/broken derived from collections and the date); the
// resolver job persists it, closes/creates tasks and notifies.
type Promises struct {
	repo     domain.PromiseRepository
	bookings bookingdomain.Repository
	tx       tx.Runner
	audit    audit.Recorder
	deps     PromiseDeps
}

func NewPromises(repo domain.PromiseRepository, bookings bookingdomain.Repository, txm tx.Runner, auditor audit.Recorder, deps PromiseDeps) *Promises {
	if deps.Log == nil {
		deps.Log = slog.Default()
	}
	return &Promises{repo: repo, bookings: bookings, tx: txm, audit: auditor, deps: deps}
}

type CreatePromiseInput struct {
	BookingID  uuid.UUID
	Amount     int64
	Currency   string
	PromisedOn time.Time
	Note       string
	ActorID    uuid.UUID
}

// followUpHour is the local hour the follow-up task falls due on promised_on.
const followUpHour = 9

const promiseBatch = 200

func (s *Promises) Create(ctx context.Context, in CreatePromiseInput) (*domain.Promise, error) {
	b, err := s.bookings.FindByID(ctx, in.BookingID)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	if b.Status == bookingdomain.StatusCancelled {
		return nil, shared.NewInvalidState("cannot add a payment promise to a cancelled booking")
	}
	bookingCurrency := strings.ToUpper(strings.TrimSpace(b.Currency))
	currency := bookingCurrency
	if strings.TrimSpace(in.Currency) != "" {
		if currency, err = fx.NormalizeCurrency(in.Currency); err != nil {
			return nil, err
		}
	}
	if currency != bookingCurrency {
		return nil, shared.NewValidation("currency must match the booking currency (" + bookingCurrency + ")")
	}
	today := s.deps.Clock.Today()
	p := &domain.Promise{
		ID: uuid.New(), BookingID: b.ID, BranchID: b.BranchID, Amount: in.Amount, Currency: currency,
		PromisedOn: fx.DateOf(in.PromisedOn), Note: strings.TrimSpace(in.Note), Status: domain.PromiseOpen,
		CreatedBy: in.ActorID, CreatedAt: time.Now().UTC(),
	}
	if err := p.ValidateNew(today); err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if s.deps.Tasks != nil {
			taskID, err := s.deps.Tasks.EnsurePromiseFollowUp(ctx, p, b.OwnerID, s.dueAt(p.PromisedOn))
			if err != nil {
				return err
			}
			p.TaskID = &taskID
		}
		if err := s.repo.InsertPromise(ctx, p); err != nil {
			return err
		}
		return s.record(ctx, in.ActorID, "payment.promise_created", p, nil)
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// List returns the booking's promises with their effective status.
func (s *Promises) List(ctx context.Context, bookingID uuid.UUID) ([]domain.Promise, error) {
	if _, err := s.bookings.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	items, err := s.repo.ListPromises(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	today := s.deps.Clock.Today()
	for i := range items {
		if items[i].Status, err = s.effective(ctx, &items[i], today); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Promises) Summary(ctx context.Context, bookingID uuid.UUID) (domain.PromiseSummary, error) {
	items, err := s.List(ctx, bookingID)
	if err != nil {
		return domain.PromiseSummary{}, err
	}
	return domain.SummarizePromises(items), nil
}

func (s *Promises) Cancel(ctx context.Context, id, actorID uuid.UUID) (*domain.Promise, error) {
	var out *domain.Promise
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		p, err := s.repo.GetPromise(ctx, id)
		if err != nil {
			return err
		}
		before := promiseSnapshot(p)
		eff, err := s.effective(ctx, p, s.deps.Clock.Today())
		if err != nil {
			return err
		}
		if eff != domain.PromiseOpen {
			return shared.NewInvalidState("promise is already " + string(eff))
		}
		if err := p.Resolve(domain.PromiseCancelled, time.Now()); err != nil {
			return err
		}
		if ok, err := s.repo.ResolvePromise(ctx, p); err != nil {
			return err
		} else if !ok {
			return shared.NewInvalidState("promise is no longer open")
		}
		if p.TaskID != nil && s.deps.Tasks != nil {
			if err := s.deps.Tasks.CloseTask(ctx, *p.TaskID, false, "promise_cancelled"); err != nil {
				return err
			}
		}
		out = p
		return s.record(ctx, actorID, "payment.promise_cancelled", p, before)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveDue persists kept/broken outcomes of open promises (job
// finance.promises_check). Failed promises are retried on the next run; the
// first error is returned after the sweep so the job is retried.
func (s *Promises) ResolveDue(ctx context.Context) (kept, broken int, err error) {
	today := s.deps.Clock.Today()
	var firstErr error
	after := uuid.Nil
	for {
		items, err := s.repo.ListOpenPromises(ctx, after, promiseBatch)
		if err != nil {
			return kept, broken, err
		}
		for i := range items {
			p := items[i]
			after = p.ID
			outcome, err := s.effective(ctx, &p, today)
			changed := false
			if err == nil && outcome != domain.PromiseOpen {
				changed, err = s.resolve(ctx, p, outcome)
			}
			switch {
			case changed && outcome == domain.PromiseKept:
				kept++
			case changed:
				broken++
			}
			if err != nil {
				s.deps.Log.ErrorContext(ctx, "payment promise resolve failed", "promise_id", p.ID, "error", err)
				firstErr = errors.Join(firstErr, err)
			}
		}
		if len(items) < promiseBatch {
			return kept, broken, firstErr
		}
	}
}

func (s *Promises) resolve(ctx context.Context, p domain.Promise, to domain.PromiseStatus) (bool, error) {
	var owner uuid.UUID
	changed := false
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		before := promiseSnapshot(&p)
		if err := p.Resolve(to, time.Now()); err != nil {
			return err
		}
		ok, err := s.repo.ResolvePromise(ctx, &p)
		if err != nil || !ok {
			return err
		}
		changed = true
		if to == domain.PromiseKept && p.TaskID != nil && s.deps.Tasks != nil {
			if err := s.deps.Tasks.CloseTask(ctx, *p.TaskID, true, "promise_kept"); err != nil {
				return err
			}
		}
		if to == domain.PromiseBroken {
			if b, err := s.bookings.FindByID(ctx, p.BookingID); err == nil {
				owner = b.OwnerID
			}
			if owner != uuid.Nil && s.deps.Tasks != nil {
				if err := s.deps.Tasks.EnsurePromiseBroken(ctx, &p, owner); err != nil {
					return err
				}
			}
		}
		return s.record(ctx, uuid.Nil, "payment.promise_"+string(to), &p, before)
	})
	if err != nil || !changed {
		return false, err
	}
	if to == domain.PromiseBroken && owner != uuid.Nil && s.deps.Notifier != nil {
		if err := s.deps.Notifier.NotifyPromiseBroken(ctx, &p, owner); err != nil {
			s.deps.Log.WarnContext(ctx, "payment promise notification failed", "promise_id", p.ID, "error", err)
		}
	}
	return true, nil
}

func (s *Promises) effective(ctx context.Context, p *domain.Promise, today time.Time) (domain.PromiseStatus, error) {
	if p.Status != domain.PromiseOpen {
		return p.Status, nil
	}
	collected, err := s.repo.SumCollectedSince(ctx, p.BookingID, p.Currency, p.CreatedAt)
	if err != nil {
		return "", err
	}
	return p.Outcome(collected, today), nil
}

func (s *Promises) dueAt(day time.Time) time.Time {
	loc := s.deps.Clock.Location
	if loc == nil {
		loc = time.UTC
	}
	return time.Date(day.Year(), day.Month(), day.Day(), followUpHour, 0, 0, 0, loc).UTC()
}

func (s *Promises) record(ctx context.Context, actor uuid.UUID, action string, p *domain.Promise, before any) error {
	if s.audit == nil {
		return nil
	}
	id, branch := p.ID, p.BranchID
	return s.audit.Record(ctx, audit.RecordInput{
		ActorID: actor, Action: action, EntityType: "payment_promise", EntityID: &id, BranchID: &branch,
		Before: before, After: promiseSnapshot(p),
	})
}
