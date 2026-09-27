package booking

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Status string

const (
	StatusDraft         Status = "draft"
	StatusQuoted        Status = "quoted"
	StatusOptionHold    Status = "option_hold"
	StatusConfirmed     Status = "confirmed"
	StatusPartiallyPaid Status = "partially_paid"
	StatusReady         Status = "ready"
	StatusTravelled     Status = "travelled"
	StatusCompleted     Status = "completed"
	StatusCancelled     Status = "cancelled"
)

// AllStatuses is the canonical lifecycle order (T-268).
func AllStatuses() []Status {
	return []Status{
		StatusDraft, StatusQuoted, StatusOptionHold, StatusConfirmed, StatusPartiallyPaid,
		StatusReady, StatusTravelled, StatusCompleted, StatusCancelled,
	}
}

func (s Status) Valid() bool {
	for _, x := range AllStatuses() {
		if x == s {
			return true
		}
	}
	return false
}

// ConsumesSeat reports whether the booking occupies departure capacity.
func (s Status) ConsumesSeat() bool {
	switch s {
	case StatusOptionHold, StatusConfirmed, StatusPartiallyPaid, StatusReady, StatusTravelled, StatusCompleted:
		return true
	default:
		return false
	}
}

// IsConfirmedFamily reports the statuses the system derives from payments
// and readiness.
func (s Status) IsConfirmedFamily() bool {
	return s == StatusConfirmed || s == StatusPartiallyPaid || s == StatusReady
}

// IsSold reports a confirmed sale (revenue, receivables, sold pax).
func (s Status) IsSold() bool {
	return s.IsConfirmedFamily() || s == StatusTravelled || s == StatusCompleted
}

func (s Status) Terminal() bool { return s == StatusCancelled || s == StatusCompleted }

// Editable reports whether pax, money and line items may still change.
func (s Status) Editable() bool { return s == StatusDraft || s == StatusQuoted }

// ActorKind is who requests a transition.
type ActorKind string

const (
	ActorUser     ActorKind = "user"
	ActorSystem   ActorKind = "system"
	ActorOverride ActorKind = "override"
)

// Guard names a failed precondition; they are part of the API contract.
type Guard string

const (
	GuardCustomerRequired       Guard = "customer_required"
	GuardDepartureRequired      Guard = "departure_required"
	GuardSalesClosed            Guard = "sales_closed"
	GuardNoCapacity             Guard = "no_capacity"
	GuardHoldExpiryRequired     Guard = "hold_expiry_required"
	GuardHoldExpiryPast         Guard = "hold_expiry_in_past"
	GuardHoldExpiryTooFar       Guard = "hold_expiry_too_far"
	GuardReasonRequired         Guard = "reason_required"
	GuardOverrideReasonTooShort Guard = "override_reason_too_short"
	GuardReadinessIncomplete    Guard = "readiness_incomplete"
	GuardBalanceOutstanding     Guard = "balance_outstanding"
	GuardHoldNotExpired         Guard = "hold_not_expired"
	GuardDepartureNotReached    Guard = "departure_not_reached"
	GuardNotDerived             Guard = "status_not_derived"
)

// Bypassable guards may be lifted by an override; the rest (capacity, hold
// window, reasons, system preconditions) are hard invariants.
func (g Guard) Bypassable() bool {
	return g == GuardReadinessIncomplete || g == GuardBalanceOutstanding
}

const (
	MaxHoldDuration      = 14 * 24 * time.Hour
	MinOverrideReasonLen = 10
)

// System transition reasons stored in status_reason.
const (
	ReasonHoldExpired = "hold_expired"
	ReasonDeparted    = "departed"
	ReasonDerived     = "derived"
)

var userEdges = map[Status][]Status{
	StatusDraft:         {StatusQuoted, StatusOptionHold, StatusConfirmed, StatusCancelled},
	StatusQuoted:        {StatusDraft, StatusOptionHold, StatusConfirmed, StatusCancelled},
	StatusOptionHold:    {StatusQuoted, StatusConfirmed, StatusCancelled},
	StatusConfirmed:     {StatusCancelled},
	StatusPartiallyPaid: {StatusCancelled},
	StatusReady:         {StatusCancelled},
	StatusTravelled:     {StatusCompleted},
}

var overrideOnlyEdges = map[Status][]Status{
	StatusConfirmed:     {StatusReady},
	StatusPartiallyPaid: {StatusReady},
}

var systemEdges = map[Status][]Status{
	StatusOptionHold:    {StatusDraft},
	StatusConfirmed:     {StatusPartiallyPaid, StatusReady, StatusTravelled},
	StatusPartiallyPaid: {StatusConfirmed, StatusReady, StatusTravelled},
	StatusReady:         {StatusConfirmed, StatusPartiallyPaid, StatusTravelled},
}

func contains(list []Status, s Status) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// CanTransition reports whether the edge exists for the actor (guards aside).
func CanTransition(from, to Status, actor ActorKind) bool {
	switch actor {
	case ActorUser:
		return contains(userEdges[from], to)
	case ActorOverride:
		return contains(userEdges[from], to) || contains(overrideOnlyEdges[from], to)
	case ActorSystem:
		return contains(systemEdges[from], to)
	default:
		return false
	}
}

// AllowedTransition is one manual option offered to a caller.
type AllowedTransition struct {
	Status           Status `json:"status"`
	RequiresReason   bool   `json:"requires_reason"`
	RequiresOverride bool   `json:"requires_override"`
}

// AllowedTransitions lists the manual transitions from s; override-only
// entries are included only for callers allowed to override.
func AllowedTransitions(s Status, canOverride bool) []AllowedTransition {
	out := make([]AllowedTransition, 0, 4)
	for _, to := range userEdges[s] {
		out = append(out, AllowedTransition{Status: to, RequiresReason: to == StatusCancelled})
	}
	if canOverride {
		for _, to := range overrideOnlyEdges[s] {
			out = append(out, AllowedTransition{Status: to, RequiresReason: true, RequiresOverride: true})
		}
	}
	return out
}

// DeriveStatus is the confirmed-family status implied by money and readiness.
func DeriveStatus(collected, balance int64, readinessOK bool) Status {
	switch {
	case readinessOK && balance <= 0:
		return StatusReady
	case collected > 0:
		return StatusPartiallyPaid
	default:
		return StatusConfirmed
	}
}

// DerivedStatus applies DeriveStatus unless ready was forced by an override.
func (b *Booking) DerivedStatus(readinessOK bool) Status {
	if b.ReadyForced {
		return StatusReady
	}
	return DeriveStatus(b.CollectedAmt, b.BalanceAmt, readinessOK)
}

// SeatChange is the departure capacity effect of a transition.
type SeatChange int

const (
	SeatNone SeatChange = iota
	SeatAcquire
	SeatRelease
)

func SeatChangeFor(from, to Status) SeatChange {
	switch {
	case !from.ConsumesSeat() && to.ConsumesSeat():
		return SeatAcquire
	case from.ConsumesSeat() && !to.ConsumesSeat():
		return SeatRelease
	default:
		return SeatNone
	}
}

// TransitionFacts are the external facts guards depend on, gathered by the
// application layer (departure, capacity under lock, readiness).
type TransitionFacts struct {
	DepartureFound   bool
	SalesOpen        bool
	HasCapacity      bool
	DepartureReached bool
	ReadinessOK      bool
}

type TransitionInput struct {
	To            Status
	Actor         ActorKind
	Reason        string
	HoldExpiresAt *time.Time
	Now           time.Time
	Facts         TransitionFacts
}

// TransitionResult describes an applied transition for audit and accounting.
type TransitionResult struct {
	From       Status
	To         Status
	Actor      ActorKind
	SeatChange SeatChange
	Bypassed   []Guard
	PrevHold   *time.Time
}

// StatusChanged is the payload of the booking.status_changed event.
type StatusChanged struct {
	Booking Booking
	From    Status
	To      Status
	Actor   ActorKind
}

// ValidOverrideReason enforces the minimum override justification (T-271).
func ValidOverrideReason(reason string) bool {
	return utf8.RuneCountInString(strings.TrimSpace(reason)) >= MinOverrideReasonLen
}

// Guards returns every failing guard of the transition, bypassable or not.
func (b *Booking) Guards(in TransitionInput) []Guard {
	var gs []Guard
	add := func(g Guard) { gs = append(gs, g) }
	to, f := in.To, in.Facts

	if in.Actor == ActorOverride && !ValidOverrideReason(in.Reason) {
		add(GuardOverrideReasonTooShort)
	}
	if to == StatusCancelled && strings.TrimSpace(in.Reason) == "" {
		add(GuardReasonRequired)
	}
	if to == StatusOptionHold {
		switch {
		case in.HoldExpiresAt == nil:
			add(GuardHoldExpiryRequired)
		case !in.HoldExpiresAt.After(in.Now):
			add(GuardHoldExpiryPast)
		case in.HoldExpiresAt.Sub(in.Now) > MaxHoldDuration:
			add(GuardHoldExpiryTooFar)
		}
	}
	if to == StatusOptionHold || (to.IsConfirmedFamily() && !b.Status.IsConfirmedFamily()) {
		if b.CustomerID == uuid.Nil {
			add(GuardCustomerRequired)
		}
		if !f.DepartureFound {
			add(GuardDepartureRequired)
		} else if SeatChangeFor(b.Status, to) == SeatAcquire {
			if !f.SalesOpen {
				add(GuardSalesClosed)
			} else if !f.HasCapacity {
				add(GuardNoCapacity)
			}
		}
	}
	if in.Actor == ActorSystem {
		switch {
		case to.IsConfirmedFamily():
			if to != b.DerivedStatus(f.ReadinessOK) {
				add(GuardNotDerived)
			}
		case to == StatusTravelled:
			if !f.DepartureReached {
				add(GuardDepartureNotReached)
			}
		case b.Status == StatusOptionHold && to == StatusDraft:
			if b.HoldExpiresAt != nil && in.Now.Before(*b.HoldExpiresAt) {
				add(GuardHoldNotExpired)
			}
		}
	} else if to == StatusReady {
		if !f.ReadinessOK {
			add(GuardReadinessIncomplete)
		}
		if b.BalanceAmt > 0 {
			add(GuardBalanceOutstanding)
		}
	}
	return gs
}

// Transition validates and applies a status change. It is the single source
// of lifecycle rules: edges per actor, guards, override bypass, hold window.
func (b *Booking) Transition(in TransitionInput) (TransitionResult, error) {
	from := b.Status
	if !in.To.Valid() {
		return TransitionResult{}, shared.NewValidation("invalid status: " + string(in.To))
	}
	if from == in.To || !CanTransition(from, in.To, in.Actor) {
		return TransitionResult{}, invalidTransition(from, in.To, in.Actor)
	}
	var hard, bypassed []Guard
	for _, g := range b.Guards(in) {
		if in.Actor == ActorOverride && g.Bypassable() {
			bypassed = append(bypassed, g)
			continue
		}
		hard = append(hard, g)
	}
	if len(hard) > 0 {
		return TransitionResult{}, guardFailed(hard)
	}

	res := TransitionResult{
		From: from, To: in.To, Actor: in.Actor,
		SeatChange: SeatChangeFor(from, in.To), Bypassed: bypassed, PrevHold: b.HoldExpiresAt,
	}
	now := in.Now.UTC()
	b.Status = in.To
	b.StatusChangedAt = now
	b.StatusReason = strings.TrimSpace(in.Reason)
	b.HoldExpiresAt = nil
	if in.To == StatusOptionHold {
		h := in.HoldExpiresAt.UTC()
		b.HoldExpiresAt = &h
	}
	switch {
	case in.Actor == ActorOverride && in.To == StatusReady:
		b.ReadyForced = true
	case !in.To.IsConfirmedFamily():
		b.ReadyForced = false
	}
	b.UpdatedAt = now
	return res, nil
}

const (
	CodeInvalidTransition = "invalid_transition"
	CodeGuardFailed       = "guard_failed"
)

func invalidTransition(from, to Status, actor ActorKind) error {
	msg := "cannot transition booking from " + string(from) + " to " + string(to)
	if actor == ActorUser && contains(overrideOnlyEdges[from], to) {
		msg += " without override"
	}
	return &shared.AppError{Code: CodeInvalidTransition, Message: msg, Err: shared.ErrConflict}
}

func guardFailed(gs []Guard) error {
	names := make([]string, len(gs))
	for i, g := range gs {
		names[i] = string(g)
	}
	return &shared.AppError{
		Code: CodeGuardFailed, Message: "transition guards failed: " + strings.Join(names, ", "),
		Err: shared.ErrInvalidState, Details: map[string]any{"guards": names},
	}
}

// FailedGuards extracts the guards of a guard_failed error.
func FailedGuards(err error) []Guard {
	var app *shared.AppError
	if !errors.As(err, &app) || app.Code != CodeGuardFailed {
		return nil
	}
	names, _ := app.Details["guards"].([]string)
	out := make([]Guard, len(names))
	for i, n := range names {
		out[i] = Guard(n)
	}
	return out
}

// ReadinessFacts are the inputs of the readiness gate.
type ReadinessFacts struct {
	ParticipantsMissing bool
	ChecklistIncomplete int
	MissingDocs         []string
	OverrideActive      bool
}

// OK reports readiness; an override lifts the checklist and document gates
// but never the participant count.
func (f ReadinessFacts) OK() bool {
	if f.ParticipantsMissing {
		return false
	}
	return f.OverrideActive || (f.ChecklistIncomplete == 0 && len(f.MissingDocs) == 0)
}

// DepartureReached reports whether today (a local calendar date) is on or
// after the departure date; both are compared as dates.
func DepartureReached(departDate, today time.Time) bool {
	dy, dm, dd := departDate.Date()
	ty, tm, td := today.Date()
	d := time.Date(dy, dm, dd, 0, 0, 0, 0, time.UTC)
	t := time.Date(ty, tm, td, 0, 0, 0, 0, time.UTC)
	return !t.Before(d)
}
