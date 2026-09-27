package booking_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type edge struct {
	from, to booking.Status
}

// specEdges restates the T-268 transition table independently of the
// implementation maps.
func specEdges() map[booking.ActorKind]map[edge]bool {
	user := map[edge]bool{}
	for _, e := range []edge{
		{booking.StatusDraft, booking.StatusQuoted}, {booking.StatusDraft, booking.StatusOptionHold},
		{booking.StatusDraft, booking.StatusConfirmed}, {booking.StatusDraft, booking.StatusCancelled},
		{booking.StatusQuoted, booking.StatusDraft}, {booking.StatusQuoted, booking.StatusOptionHold},
		{booking.StatusQuoted, booking.StatusConfirmed}, {booking.StatusQuoted, booking.StatusCancelled},
		{booking.StatusOptionHold, booking.StatusQuoted}, {booking.StatusOptionHold, booking.StatusConfirmed},
		{booking.StatusOptionHold, booking.StatusCancelled},
		{booking.StatusConfirmed, booking.StatusCancelled}, {booking.StatusPartiallyPaid, booking.StatusCancelled},
		{booking.StatusReady, booking.StatusCancelled},
		{booking.StatusTravelled, booking.StatusCompleted},
	} {
		user[e] = true
	}
	override := map[edge]bool{
		{booking.StatusConfirmed, booking.StatusReady}:     true,
		{booking.StatusPartiallyPaid, booking.StatusReady}: true,
	}
	for e := range user {
		override[e] = true
	}
	system := map[edge]bool{
		{booking.StatusOptionHold, booking.StatusDraft}: true,
	}
	family := []booking.Status{booking.StatusConfirmed, booking.StatusPartiallyPaid, booking.StatusReady}
	for _, f := range family {
		for _, t := range family {
			if f != t {
				system[edge{f, t}] = true
			}
		}
		system[edge{f, booking.StatusTravelled}] = true
	}
	return map[booking.ActorKind]map[edge]bool{
		booking.ActorUser: user, booking.ActorOverride: override, booking.ActorSystem: system,
	}
}

var now = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

// satisfying builds a booking and input for which every guard of from→to passes.
func satisfying(from, to booking.Status, actor booking.ActorKind) (*booking.Booking, booking.TransitionInput) {
	b := &booking.Booking{ID: uuid.New(), CustomerID: uuid.New(), DepartureID: uuid.New(), Status: from,
		PaxCount: 2, TotalAmount: 100, BalanceAmt: 0, CollectedAmt: 100}
	if from == booking.StatusOptionHold {
		h := now.Add(-time.Hour)
		b.HoldExpiresAt = &h
	}
	switch to {
	case booking.StatusConfirmed:
		b.CollectedAmt, b.BalanceAmt = 0, 100
	case booking.StatusPartiallyPaid:
		b.CollectedAmt, b.BalanceAmt = 50, 50
	}
	in := booking.TransitionInput{
		To: to, Actor: actor, Reason: "customer asked by phone", Now: now,
		Facts: booking.TransitionFacts{DepartureFound: true, SalesOpen: true, HasCapacity: true, DepartureReached: true, ReadinessOK: true},
	}
	if to == booking.StatusOptionHold {
		h := now.Add(48 * time.Hour)
		in.HoldExpiresAt = &h
	}
	return b, in
}

func appCode(err error) string {
	var app *shared.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func TestTransitionMatrix(t *testing.T) {
	spec := specEdges()
	for _, actor := range []booking.ActorKind{booking.ActorUser, booking.ActorSystem, booking.ActorOverride} {
		for _, from := range booking.AllStatuses() {
			for _, to := range booking.AllStatuses() {
				want := spec[actor][edge{from, to}]
				if got := booking.CanTransition(from, to, actor); got != want {
					t.Errorf("CanTransition(%s→%s, %s)=%v want %v", from, to, actor, got, want)
				}
				b, in := satisfying(from, to, actor)
				_, err := b.Transition(in)
				switch {
				case want && err != nil:
					t.Errorf("%s→%s as %s: unexpected error %v", from, to, actor, err)
				case want && b.Status != to:
					t.Errorf("%s→%s as %s: status=%s", from, to, actor, b.Status)
				case !want && appCode(err) != booking.CodeInvalidTransition:
					t.Errorf("%s→%s as %s: want invalid_transition, got %v", from, to, actor, err)
				case !want && b.Status != from:
					t.Errorf("%s→%s as %s: rejected transition mutated status", from, to, actor)
				}
			}
		}
	}
}

func TestTerminalStatusesHaveNoExits(t *testing.T) {
	for _, s := range []booking.Status{booking.StatusCancelled, booking.StatusCompleted} {
		for _, actor := range []booking.ActorKind{booking.ActorUser, booking.ActorSystem, booking.ActorOverride} {
			for _, to := range booking.AllStatuses() {
				if booking.CanTransition(s, to, actor) {
					t.Fatalf("%s is terminal but %s→%s allowed for %s", s, s, to, actor)
				}
			}
		}
		if !s.Terminal() {
			t.Fatalf("%s must be terminal", s)
		}
	}
}

func TestGuards(t *testing.T) {
	type mod func(b *booking.Booking, in *booking.TransitionInput)
	cases := []struct {
		name     string
		from, to booking.Status
		actor    booking.ActorKind
		mod      mod
		want     []booking.Guard
		bypassed []booking.Guard
	}{
		{"confirm without capacity", booking.StatusDraft, booking.StatusConfirmed, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Facts.HasCapacity = false },
			[]booking.Guard{booking.GuardNoCapacity}, nil},
		{"capacity is not overridable", booking.StatusQuoted, booking.StatusConfirmed, booking.ActorOverride,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Facts.HasCapacity = false },
			[]booking.Guard{booking.GuardNoCapacity}, nil},
		{"hold without capacity", booking.StatusDraft, booking.StatusOptionHold, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Facts.HasCapacity = false },
			[]booking.Guard{booking.GuardNoCapacity}, nil},
		{"held seat confirms without free capacity", booking.StatusOptionHold, booking.StatusConfirmed, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) {
				in.Facts.HasCapacity, in.Facts.SalesOpen = false, false
			}, nil, nil},
		{"sales closed", booking.StatusDraft, booking.StatusConfirmed, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Facts.SalesOpen = false },
			[]booking.Guard{booking.GuardSalesClosed}, nil},
		{"customer and departure required", booking.StatusDraft, booking.StatusConfirmed, booking.ActorUser,
			func(b *booking.Booking, in *booking.TransitionInput) {
				b.CustomerID = uuid.Nil
				in.Facts.DepartureFound = false
			}, []booking.Guard{booking.GuardCustomerRequired, booking.GuardDepartureRequired}, nil},
		{"hold expiry required", booking.StatusDraft, booking.StatusOptionHold, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.HoldExpiresAt = nil },
			[]booking.Guard{booking.GuardHoldExpiryRequired}, nil},
		{"hold expiry in past", booking.StatusQuoted, booking.StatusOptionHold, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) { h := now; in.HoldExpiresAt = &h },
			[]booking.Guard{booking.GuardHoldExpiryPast}, nil},
		{"hold expiry beyond 14 days", booking.StatusQuoted, booking.StatusOptionHold, booking.ActorOverride,
			func(_ *booking.Booking, in *booking.TransitionInput) {
				h := now.Add(booking.MaxHoldDuration + time.Minute)
				in.HoldExpiresAt = &h
			}, []booking.Guard{booking.GuardHoldExpiryTooFar}, nil},
		{"hold expiry at 14 days", booking.StatusQuoted, booking.StatusOptionHold, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) {
				h := now.Add(booking.MaxHoldDuration)
				in.HoldExpiresAt = &h
			}, nil, nil},
		{"cancel needs reason", booking.StatusConfirmed, booking.StatusCancelled, booking.ActorUser,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Reason = "  " },
			[]booking.Guard{booking.GuardReasonRequired}, nil},
		{"override reason too short", booking.StatusConfirmed, booking.StatusReady, booking.ActorOverride,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Reason = "vip trip" },
			[]booking.Guard{booking.GuardOverrideReasonTooShort}, nil},
		{"override forces ready", booking.StatusPartiallyPaid, booking.StatusReady, booking.ActorOverride,
			func(b *booking.Booking, in *booking.TransitionInput) {
				b.BalanceAmt = 40
				in.Facts.ReadinessOK = false
			}, nil, []booking.Guard{booking.GuardReadinessIncomplete, booking.GuardBalanceOutstanding}},
		{"system derived mismatch", booking.StatusConfirmed, booking.StatusReady, booking.ActorSystem,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Facts.ReadinessOK = false },
			[]booking.Guard{booking.GuardNotDerived}, nil},
		{"travelled before departure", booking.StatusReady, booking.StatusTravelled, booking.ActorSystem,
			func(_ *booking.Booking, in *booking.TransitionInput) { in.Facts.DepartureReached = false },
			[]booking.Guard{booking.GuardDepartureNotReached}, nil},
		{"travelled regardless of readiness", booking.StatusPartiallyPaid, booking.StatusTravelled, booking.ActorSystem,
			func(b *booking.Booking, in *booking.TransitionInput) {
				b.BalanceAmt = 50
				in.Facts.ReadinessOK = false
			}, nil, nil},
		{"hold not expired", booking.StatusOptionHold, booking.StatusDraft, booking.ActorSystem,
			func(b *booking.Booking, _ *booking.TransitionInput) { h := now.Add(time.Minute); b.HoldExpiresAt = &h },
			[]booking.Guard{booking.GuardHoldNotExpired}, nil},
	}
	for _, c := range cases {
		b, in := satisfying(c.from, c.to, c.actor)
		c.mod(b, &in)
		res, err := b.Transition(in)
		got := booking.FailedGuards(err)
		if len(c.want) == 0 {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
				continue
			}
		} else if !reflect.DeepEqual(got, c.want) || appCode(err) != booking.CodeGuardFailed {
			t.Errorf("%s: guards=%v err=%v want %v", c.name, got, err, c.want)
			continue
		}
		if !reflect.DeepEqual(res.Bypassed, c.bypassed) {
			t.Errorf("%s: bypassed=%v want %v", c.name, res.Bypassed, c.bypassed)
		}
	}
}

func TestUserCannotRequestReadyWithoutOverride(t *testing.T) {
	b, in := satisfying(booking.StatusConfirmed, booking.StatusReady, booking.ActorUser)
	_, err := b.Transition(in)
	if appCode(err) != booking.CodeInvalidTransition {
		t.Fatalf("want invalid_transition, got %v", err)
	}
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(app.Err, shared.ErrConflict) {
		t.Fatalf("invalid_transition must map to conflict: %v", err)
	}
}

func TestTransitionSideEffects(t *testing.T) {
	b, in := satisfying(booking.StatusQuoted, booking.StatusOptionHold, booking.ActorUser)
	res, err := b.Transition(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.SeatChange != booking.SeatAcquire || b.HoldExpiresAt == nil || !b.HoldExpiresAt.Equal(*in.HoldExpiresAt) {
		t.Fatalf("hold not recorded: %+v %v", res, b.HoldExpiresAt)
	}
	if !b.StatusChangedAt.Equal(now) || b.StatusReason != "customer asked by phone" {
		t.Fatalf("status metadata: %v %q", b.StatusChangedAt, b.StatusReason)
	}
	res, err = b.Transition(booking.TransitionInput{To: booking.StatusQuoted, Actor: booking.ActorUser, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if res.SeatChange != booking.SeatRelease || b.HoldExpiresAt != nil || res.PrevHold == nil {
		t.Fatalf("hold not cleared: %+v %v", res, b.HoldExpiresAt)
	}

	b, in = satisfying(booking.StatusConfirmed, booking.StatusReady, booking.ActorOverride)
	b.BalanceAmt = 10
	in.Facts.ReadinessOK = false
	if _, err := b.Transition(in); err != nil {
		t.Fatal(err)
	}
	if !b.ReadyForced || b.DerivedStatus(false) != booking.StatusReady {
		t.Fatal("override must pin ready")
	}
	if _, err := b.Transition(booking.TransitionInput{To: booking.StatusCancelled, Actor: booking.ActorUser, Reason: "trip cancelled", Now: now}); err != nil {
		t.Fatal(err)
	}
	if b.ReadyForced {
		t.Fatal("leaving the confirmed family must clear the forced ready flag")
	}
}

func TestDeriveStatus(t *testing.T) {
	cases := []struct {
		collected, balance int64
		ready              bool
		want               booking.Status
	}{
		{0, 100, false, booking.StatusConfirmed},
		{0, 100, true, booking.StatusConfirmed},
		{40, 60, false, booking.StatusPartiallyPaid},
		{40, 60, true, booking.StatusPartiallyPaid},
		{100, 0, false, booking.StatusPartiallyPaid},
		{100, 0, true, booking.StatusReady},
		{0, 0, true, booking.StatusReady},
		{0, 0, false, booking.StatusConfirmed},
		{-10, 110, true, booking.StatusConfirmed},
	}
	for _, c := range cases {
		if got := booking.DeriveStatus(c.collected, c.balance, c.ready); got != c.want {
			t.Errorf("derive(%d,%d,%v)=%s want %s", c.collected, c.balance, c.ready, got, c.want)
		}
	}
}

func TestSeatChangeFor(t *testing.T) {
	seat := map[booking.Status]bool{
		booking.StatusOptionHold: true, booking.StatusConfirmed: true, booking.StatusPartiallyPaid: true,
		booking.StatusReady: true, booking.StatusTravelled: true, booking.StatusCompleted: true,
	}
	for _, from := range booking.AllStatuses() {
		if from.ConsumesSeat() != seat[from] {
			t.Fatalf("%s ConsumesSeat=%v", from, from.ConsumesSeat())
		}
		for _, to := range booking.AllStatuses() {
			want := booking.SeatNone
			if !seat[from] && seat[to] {
				want = booking.SeatAcquire
			} else if seat[from] && !seat[to] {
				want = booking.SeatRelease
			}
			if got := booking.SeatChangeFor(from, to); got != want {
				t.Errorf("%s→%s seat=%v want %v", from, to, got, want)
			}
		}
	}
}

func TestAllowedTransitions(t *testing.T) {
	got := booking.AllowedTransitions(booking.StatusPartiallyPaid, false)
	want := []booking.AllowedTransition{{Status: booking.StatusCancelled, RequiresReason: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("without override: %+v", got)
	}
	got = booking.AllowedTransitions(booking.StatusPartiallyPaid, true)
	want = append(want, booking.AllowedTransition{Status: booking.StatusReady, RequiresReason: true, RequiresOverride: true})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with override: %+v", got)
	}
	if n := len(booking.AllowedTransitions(booking.StatusCompleted, true)); n != 0 {
		t.Fatalf("completed offers %d transitions", n)
	}
	spec := specEdges()
	for _, s := range booking.AllStatuses() {
		for _, a := range booking.AllowedTransitions(s, true) {
			actor := booking.ActorUser
			if a.RequiresOverride {
				actor = booking.ActorOverride
			}
			if !spec[actor][edge{s, a.Status}] {
				t.Fatalf("offered %s→%s not in spec", s, a.Status)
			}
		}
	}
}

func TestReadinessFacts(t *testing.T) {
	cases := []struct {
		f    booking.ReadinessFacts
		want bool
	}{
		{booking.ReadinessFacts{}, true},
		{booking.ReadinessFacts{ChecklistIncomplete: 1}, false},
		{booking.ReadinessFacts{MissingDocs: []string{"visa"}}, false},
		{booking.ReadinessFacts{MissingDocs: []string{"visa"}, ChecklistIncomplete: 2, OverrideActive: true}, true},
		{booking.ReadinessFacts{ParticipantsMissing: true, OverrideActive: true}, false},
	}
	for i, c := range cases {
		if c.f.OK() != c.want {
			t.Errorf("case %d: OK=%v want %v", i, c.f.OK(), c.want)
		}
	}
}

func TestDepartureReached(t *testing.T) {
	dep := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if booking.DepartureReached(dep, time.Date(2026, 9, 30, 23, 59, 0, 0, time.UTC)) {
		t.Fatal("day before departure")
	}
	if !booking.DepartureReached(dep, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("departure day")
	}
	riyadh := time.FixedZone("AST", 3*3600)
	if !booking.DepartureReached(dep, time.Date(2026, 10, 1, 1, 0, 0, 0, riyadh)) {
		t.Fatal("local calendar date must be used")
	}
}
