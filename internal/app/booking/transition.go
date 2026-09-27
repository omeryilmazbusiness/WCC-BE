package booking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	pkgdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

// TransitionInput is a manual status change request (T-268/T-271).
type TransitionInput struct {
	Status        domain.Status
	Reason        string
	HoldExpiresAt *time.Time
	// Override is set only for callers holding bookings.override.
	Override bool
}

// Transition applies a manual status change. Entering the confirmed family
// immediately settles on the derived status (e.g. confirmed → partially_paid
// when money was already collected) as a separate system transition.
func (s *Service) Transition(ctx context.Context, bookingID uuid.UUID, in TransitionInput) (*domain.Booking, error) {
	actor := domain.ActorUser
	if in.Override {
		actor = domain.ActorOverride
	}
	var out *domain.Booking
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, bookingID)
		if err != nil {
			return err
		}
		if err := s.apply(ctx, b, change{in: domain.TransitionInput{
			To: in.Status, Actor: actor, Reason: in.Reason, HoldExpiresAt: in.HoldExpiresAt, Now: s.now(),
		}}, &evs); err != nil {
			return err
		}
		if err := s.rederive(ctx, b, nil, &evs); err != nil {
			return err
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, evs)
	return out, nil
}

// Confirm is the legacy confirm endpoint; it is a plain manual transition.
func (s *Service) Confirm(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	return s.Transition(ctx, bookingID, TransitionInput{Status: domain.StatusConfirmed})
}

// Recompute settles a confirmed-family booking on the status derived from
// its payments and readiness (T-269). It is idempotent.
func (s *Service) Recompute(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	b, _, err := s.recompute(ctx, bookingID)
	return b, err
}

func (s *Service) recompute(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, bool, error) {
	var out *domain.Booking
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, bookingID)
		if err != nil {
			return err
		}
		out = b
		return s.rederive(ctx, b, nil, &evs)
	})
	if err != nil {
		return nil, false, err
	}
	s.publish(ctx, evs)
	return out, len(evs) > 0, nil
}

// recomputeLocked is Recompute inside an existing unit of work.
func (s *Service) recomputeLocked(ctx context.Context, bookingID uuid.UUID, evs *[]events.Event) error {
	b, err := s.store.FindForUpdate(ctx, bookingID)
	if err != nil {
		return err
	}
	return s.rederive(ctx, b, nil, evs)
}

func (s *Service) rederive(ctx context.Context, b *domain.Booking, facts *domain.ReadinessFacts, evs *[]events.Event) error {
	if !b.Status.IsConfirmedFamily() {
		return nil
	}
	if facts == nil {
		f, err := s.readinessFacts(ctx, b)
		if err != nil {
			return err
		}
		facts = &f
	}
	target := b.DerivedStatus(facts.OK())
	if target == b.Status {
		return nil
	}
	return s.apply(ctx, b, change{
		in:        domain.TransitionInput{To: target, Actor: domain.ActorSystem, Reason: domain.ReasonDerived, Now: s.now()},
		readiness: facts,
		extra: map[string]any{
			"collected_amt": b.CollectedAmt, "balance_amt": b.BalanceAmt,
			"readiness_ok": facts.OK(), "ready_forced": b.ReadyForced,
		},
	}, evs)
}

// change is one transition request inside a unit of work.
type change struct {
	in        domain.TransitionInput
	readiness *domain.ReadinessFacts
	extra     map[string]any
}

func departureFacts(dep *pkgdomain.Departure, sold, pax int, today time.Time) domain.TransitionFacts {
	return domain.TransitionFacts{
		DepartureFound:   true,
		SalesOpen:        dep.IsActive && !dep.SalesClosed,
		HasCapacity:      dep.AllowOversell || sold+pax <= dep.CapacityTotal,
		DepartureReached: domain.DepartureReached(dep.DepartDate, today),
	}
}

// apply gathers guard facts, applies the domain transition, keeps departure
// capacity in sync and audits, all inside the caller's transaction. The
// booking must have been loaded with FindForUpdate.
func (s *Service) apply(ctx context.Context, b *domain.Booking, c change, evs *[]events.Event) error {
	to := c.in.To
	if !to.Valid() {
		return shared.NewValidation("invalid status: " + string(to))
	}
	seat := domain.SeatChangeFor(b.Status, to)
	soldBefore := -1
	departureMissing := false
	if seat != domain.SeatNone {
		if err := s.store.LockDeparture(ctx, b.DepartureID); err != nil {
			if !errors.Is(err, shared.ErrNotFound) {
				return err
			}
			departureMissing = true
		}
		sold, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
		if err != nil {
			return err
		}
		soldBefore = sold
	}
	needsDeparture := to == domain.StatusTravelled || (to.ConsumesSeat() && !b.Status.IsConfirmedFamily())
	if needsDeparture && !departureMissing {
		if dep, err := s.departures.FindDeparture(ctx, b.DepartureID); err == nil {
			c.in.Facts = departureFacts(dep, max(soldBefore, 0), b.PaxCount, c.in.Now.In(s.loc))
		}
	}
	if to == domain.StatusReady || (c.in.Actor == domain.ActorSystem && to.IsConfirmedFamily()) {
		if c.readiness == nil {
			f, err := s.readinessFacts(ctx, b)
			if err != nil {
				return err
			}
			c.readiness = &f
		}
		c.in.Facts.ReadinessOK = c.readiness.OK()
	}

	res, err := b.Transition(c.in)
	if err != nil {
		return err
	}
	if err := s.store.SaveStatus(ctx, b); err != nil {
		return err
	}
	soldAfter := -1
	if seat != domain.SeatNone {
		if soldAfter, err = s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID); err != nil {
			return err
		}
		if err := s.departures.UpdateDepartureCapacitySold(ctx, b.DepartureID, soldAfter); err != nil {
			return err
		}
	}
	actx := ctx
	if res.Actor == domain.ActorSystem {
		actx = audit.AsSystem(ctx)
	}
	if err := s.recordTransition(actx, b, res, soldBefore, soldAfter, c.extra); err != nil {
		return err
	}

	snap := *b
	*evs = append(*evs, events.Event{Name: events.BookingStatusChanged, Payload: &domain.StatusChanged{
		Booking: snap, From: res.From, To: res.To, Actor: res.Actor,
	}})
	switch {
	case to.IsConfirmedFamily() && !res.From.IsConfirmedFamily():
		*evs = append(*evs, events.Event{Name: events.BookingConfirmed, Payload: &snap})
	case to == domain.StatusCancelled:
		*evs = append(*evs, events.Event{Name: events.BookingCancelled, Payload: &snap})
	}
	return nil
}

func (s *Service) publish(ctx context.Context, evs []events.Event) {
	for _, ev := range evs {
		s.bus.Publish(ctx, ev)
	}
}
