package booking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

// HoldFollowUps creates the owner's follow-up task when an option hold
// lapses; it runs inside the expiry transaction.
type HoldFollowUps interface {
	EnsureHoldExpiredTask(ctx context.Context, branchID, bookingID, ownerID uuid.UUID, expiredAt time.Time) error
}

// Lifecycle runs the automatic transitions (T-269): derived recompute,
// option hold expiry, travelled on departure and drift reconciliation.
type Lifecycle struct {
	svc   *Service
	tasks HoldFollowUps
	batch int
}

func NewLifecycle(svc *Service, tasks HoldFollowUps) *Lifecycle {
	return &Lifecycle{svc: svc, tasks: tasks, batch: 200}
}

// SweepResult summarizes one sweep run.
type SweepResult struct {
	Scanned      int
	Transitioned int
	Failed       int
}

func systemCtx(ctx context.Context) context.Context {
	return audit.AsSystem(access.WithScope(ctx, access.System()))
}

// Register recomputes a booking after every payment event the payment
// service publishes; the hourly reconcile sweep covers ledger changes that
// publish no event.
func (l *Lifecycle) Register(bus *events.Bus) {
	bus.Subscribe(events.PaymentRecorded, l.onPayment)
}

func (l *Lifecycle) onPayment(ctx context.Context, ev events.Event) error {
	p, ok := ev.Payload.(*paymentdomain.Payment)
	if !ok || p == nil {
		return nil
	}
	_, err := l.Recompute(ctx, p.BookingID)
	return err
}

func (l *Lifecycle) Recompute(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	return l.svc.Recompute(systemCtx(ctx), bookingID)
}

// ExpireHolds returns lapsed option holds to draft, releasing their seats.
func (l *Lifecycle) ExpireHolds(ctx context.Context, now time.Time) (SweepResult, error) {
	ctx = systemCtx(ctx)
	return l.sweep(ctx, func(after uuid.UUID) ([]uuid.UUID, error) {
		return l.svc.store.ListExpiredHolds(ctx, now, after, l.batch)
	}, func(id uuid.UUID) (bool, error) {
		return l.expireHold(ctx, id, now)
	})
}

// MarkTravelled moves confirmed-family bookings whose departure date (in
// the business time zone) has been reached to travelled, ready or not.
func (l *Lifecycle) MarkTravelled(ctx context.Context, now time.Time) (SweepResult, error) {
	ctx = systemCtx(ctx)
	today := now.In(l.svc.loc)
	return l.sweep(ctx, func(after uuid.UUID) ([]uuid.UUID, error) {
		return l.svc.store.ListDueForTravel(ctx, today, after, l.batch)
	}, func(id uuid.UUID) (bool, error) {
		return l.markTravelled(ctx, id, now)
	})
}

// ReconcileDerived re-derives confirmed-family bookings whose status may
// have drifted (ledger corrections, document reviews).
func (l *Lifecycle) ReconcileDerived(ctx context.Context) (SweepResult, error) {
	ctx = systemCtx(ctx)
	return l.sweep(ctx, func(after uuid.UUID) ([]uuid.UUID, error) {
		return l.svc.store.ListDerivedCandidates(ctx, after, l.batch)
	}, func(id uuid.UUID) (bool, error) {
		_, changed, err := l.svc.recompute(ctx, id)
		return changed, err
	})
}

func (l *Lifecycle) sweep(ctx context.Context, list func(after uuid.UUID) ([]uuid.UUID, error), one func(uuid.UUID) (bool, error)) (SweepResult, error) {
	var res SweepResult
	var firstErr error
	after := uuid.Nil
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		ids, err := list(after)
		if err != nil {
			return res, err
		}
		for _, id := range ids {
			res.Scanned++
			changed, err := one(id)
			if err != nil {
				res.Failed++
				if firstErr == nil {
					firstErr = fmt.Errorf("booking %s: %w", id, err)
				}
				continue
			}
			if changed {
				res.Transitioned++
			}
		}
		if len(ids) < l.batch {
			break
		}
		after = ids[len(ids)-1]
	}
	if firstErr != nil {
		return res, fmt.Errorf("%d of %d bookings failed: %w", res.Failed, res.Scanned, firstErr)
	}
	return res, nil
}

func (l *Lifecycle) expireHold(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	s := l.svc
	changed := false
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if b.Status != domain.StatusOptionHold || b.HoldExpiresAt == nil || now.Before(*b.HoldExpiresAt) {
			return nil
		}
		expiredAt := *b.HoldExpiresAt
		if err := s.apply(ctx, b, change{
			in:    domain.TransitionInput{To: domain.StatusDraft, Actor: domain.ActorSystem, Reason: domain.ReasonHoldExpired, Now: now},
			extra: map[string]any{"hold_expires_at": expiredAt},
		}, &evs); err != nil {
			return err
		}
		if l.tasks != nil {
			if err := l.tasks.EnsureHoldExpiredTask(ctx, b.BranchID, b.ID, b.OwnerID, expiredAt); err != nil {
				return err
			}
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	s.publish(ctx, evs)
	return changed, nil
}

func (l *Lifecycle) markTravelled(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	s := l.svc
	changed := false
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if !b.Status.IsConfirmedFamily() {
			return nil
		}
		facts, err := s.readinessFacts(ctx, b)
		if err != nil {
			return err
		}
		err = s.apply(ctx, b, change{
			in:        domain.TransitionInput{To: domain.StatusTravelled, Actor: domain.ActorSystem, Reason: domain.ReasonDeparted, Now: now},
			readiness: &facts,
			extra: map[string]any{
				"readiness_ok": facts.OK(), "balance_amt": b.BalanceAmt, "collected_amt": b.CollectedAmt,
				"ready_forced": b.ReadyForced, "missing_docs": facts.MissingDocs,
				"checklist_incomplete": facts.ChecklistIncomplete, "participants_missing": facts.ParticipantsMissing,
			},
		}, &evs)
		if gs := domain.FailedGuards(err); len(gs) == 1 && gs[0] == domain.GuardDepartureNotReached {
			return nil
		}
		if err != nil {
			return err
		}
		changed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	s.publish(ctx, evs)
	return changed, nil
}

// Job handlers; signatures match worker.JobHandler.

func (l *Lifecycle) HandleHoldExpiry(ctx context.Context, _ []byte) error {
	_, err := l.ExpireHolds(ctx, l.svc.now())
	return err
}

func (l *Lifecycle) HandleTravelledSweep(ctx context.Context, _ []byte) error {
	_, err := l.MarkTravelled(ctx, l.svc.now())
	return err
}

func (l *Lifecycle) HandleRecomputeSweep(ctx context.Context, _ []byte) error {
	_, err := l.ReconcileDerived(ctx)
	return err
}

func (l *Lifecycle) HandleRecompute(ctx context.Context, payload []byte) error {
	var p domain.RecomputePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("decode booking.recompute: %w", err)
	}
	if p.BookingID == uuid.Nil {
		return fmt.Errorf("booking.recompute: booking_id is required")
	}
	_, err := l.Recompute(ctx, p.BookingID)
	if errors.Is(err, shared.ErrNotFound) {
		return nil
	}
	return err
}
