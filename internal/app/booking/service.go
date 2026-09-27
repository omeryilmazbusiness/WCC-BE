package booking

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	pkgdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID    uuid.UUID
	CustomerID  uuid.UUID
	DepartureID uuid.UUID
	LeadID      *uuid.UUID
	PaxCount    int
	TotalAmount int64
	DiscountAmt int64
	Currency    string
	Notes       string
	OwnerID     uuid.UUID
	// CanDiscount is set for callers holding bookings.discount.
	CanDiscount bool
}

type UpdateInput struct {
	PaxCount    int
	TotalAmount int64
	Currency    string
	DiscountAmt *int64
	Notes       *string
	// CanDiscount is set for callers holding bookings.discount.
	CanDiscount bool
}

type AddParticipantInput struct {
	FullName    string
	PassportNo  string
	Nationality string
	DateOfBirth *time.Time
}

type UpdateParticipantInput struct {
	FullName string
	// PassportNo nil or masked keeps the stored passport.
	PassportNo  *string
	Nationality string
	DateOfBirth *time.Time
}

type LineItemInput struct {
	Kind      string
	Category  string
	Label     string
	Quantity  int
	UnitPrice int64
	UnitCost  int64
}

type ChecklistUpdateInput struct {
	Completed bool
}

type ListInput struct {
	BranchID    *uuid.UUID
	CustomerID  *uuid.UUID
	DepartureID *uuid.UUID
	OwnerID     *uuid.UUID
	LeadID      *uuid.UUID
	Status      domain.Status
	Query       string
	Limit       int
	Offset      int
}

type Service struct {
	repo       domain.Repository
	store      domain.LifecycleStore
	departures pkgdomain.Repository
	tx         tx.Runner
	bus        *events.Bus
	audit      audit.Recorder
	docs       DocReadiness
	now        func() time.Time
	loc        *time.Location
}

// DocReadiness evaluates policy-required documents for the readiness gate (Epic 12).
type DocReadiness interface {
	MissingRequiredKinds(ctx context.Context, bookingID uuid.UUID) ([]string, error)
}

func NewService(
	repo domain.Repository,
	store domain.LifecycleStore,
	departures pkgdomain.Repository,
	txm tx.Runner,
	bus *events.Bus,
) *Service {
	return &Service{
		repo: repo, store: store, departures: departures, tx: txm, bus: bus,
		now: func() time.Time { return time.Now().UTC() }, loc: time.UTC,
	}
}

func (s *Service) SetAuditor(a audit.Recorder)    { s.audit = a }
func (s *Service) SetDocReadiness(d DocReadiness) { s.docs = d }

// SetClock replaces the time source (tests).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetTimeZone sets the business time zone that decides the local
// departure date for the travelled transition.
func (s *Service) SetTimeZone(loc *time.Location) {
	if loc != nil {
		s.loc = loc
	}
}

func errDiscountForbidden() error {
	return shared.NewForbidden("bookings.discount permission required to change discount")
}

func (s *Service) CreateDraft(ctx context.Context, in CreateInput) (*domain.Booking, error) {
	if in.PaxCount <= 0 {
		return nil, shared.NewValidation("pax_count must be > 0")
	}
	if in.DiscountAmt != 0 && !in.CanDiscount {
		return nil, errDiscountForbidden()
	}
	if in.CustomerID == uuid.Nil || in.DepartureID == uuid.Nil {
		return nil, shared.NewValidation("customer_id and departure_id are required")
	}
	dep, err := s.departures.FindDeparture(ctx, in.DepartureID)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	if in.Currency == "" {
		in.Currency = dep.Currency
	}
	if in.Currency == "" {
		in.Currency = "USD"
	}
	if in.DiscountAmt < 0 {
		return nil, shared.NewValidation("discount_amt must be >= 0")
	}
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, err
	}
	branchID, err := scope.WriteBranch(in.BranchID)
	if err != nil {
		return nil, err
	}
	ownerID, err := scope.ResolveOwner(in.OwnerID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	b := &domain.Booking{
		ID:              uuid.New(),
		BranchID:        branchID,
		CustomerID:      in.CustomerID,
		DepartureID:     in.DepartureID,
		LeadID:          in.LeadID,
		Status:          domain.StatusDraft,
		PaxCount:        in.PaxCount,
		TotalAmount:     in.TotalAmount,
		DiscountAmt:     in.DiscountAmt,
		Currency:        in.Currency,
		Notes:           strings.TrimSpace(in.Notes),
		OwnerID:         ownerID,
		StatusChangedAt: now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	b.RecomputeBalance()

	checklist := domain.DefaultChecklist(b.ID)
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, b); err != nil {
			return err
		}
		if err := s.repo.SeedChecklist(ctx, checklist); err != nil {
			return err
		}
		return s.recordBooking(ctx, "booking.created", b, nil, bookingSnapshot(b), nil)
	}); err != nil {
		return nil, err
	}

	s.bus.Publish(ctx, events.Event{Name: events.BookingDrafted, Payload: b})
	return b, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Booking, error) {
	b, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return b, nil
}

// List pins the branch filter to the caller's scope; the repository also
// restricts owners, so in.OwnerID can only narrow visibility.
func (s *Service) List(ctx context.Context, in ListInput) ([]domain.Booking, int, error) {
	scope, err := access.Require(ctx)
	if err != nil {
		return nil, 0, err
	}
	branchID, err := scope.ResolveBranch(in.BranchID)
	if err != nil {
		return nil, 0, err
	}
	f := domain.ListFilter{
		BranchID: branchID, CustomerID: in.CustomerID, DepartureID: in.DepartureID, OwnerID: in.OwnerID,
		LeadID: in.LeadID, Status: in.Status, Query: in.Query, Limit: in.Limit, Offset: in.Offset,
	}
	return s.repo.List(ctx, f)
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*domain.Booking, error) {
	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, id)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		before := bookingSnapshot(b)
		prevDiscount := b.DiscountAmt
		if in.DiscountAmt != nil && *in.DiscountAmt != prevDiscount && !in.CanDiscount {
			return errDiscountForbidden()
		}
		lines, err := s.repo.ListLineItems(ctx, id)
		if err != nil {
			return err
		}
		if err := b.ApplyUpdate(domain.UpdateFields{
			PaxCount: in.PaxCount, TotalAmount: in.TotalAmount, Currency: in.Currency,
			DiscountAmt: in.DiscountAmt, Notes: in.Notes,
		}, lines, s.now()); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		if err := s.recordBooking(ctx, "booking.updated", b, before, bookingSnapshot(b), nil); err != nil {
			return err
		}
		if b.DiscountAmt != prevDiscount {
			if err := s.recordBooking(ctx, "booking.discount_changed", b,
				map[string]any{"discount_amt": prevDiscount}, map[string]any{"discount_amt": b.DiscountAmt},
				map[string]any{"currency": b.Currency}); err != nil {
				return err
			}
		}
		out = b
		return nil
	})
	return out, err
}

func (s *Service) AddParticipant(ctx context.Context, bookingID uuid.UUID, in AddParticipantInput) (*domain.Participant, error) {
	name := strings.TrimSpace(in.FullName)
	if name == "" {
		return nil, shared.NewValidation("full_name is required")
	}
	var out *domain.Participant
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status.Terminal() {
			return shared.NewInvalidState("cannot add participants to a " + string(b.Status) + " booking")
		}
		existing, err := s.repo.ListParticipants(ctx, bookingID)
		if err != nil {
			return err
		}
		if len(existing) >= b.PaxCount {
			return shared.NewConflict("participant count would exceed pax_count")
		}
		p := &domain.Participant{
			ID:          uuid.New(),
			BookingID:   bookingID,
			FullName:    name,
			PassportNo:  strings.TrimSpace(in.PassportNo),
			Nationality: strings.TrimSpace(in.Nationality),
			DateOfBirth: in.DateOfBirth,
			CreatedAt:   s.now(),
		}
		if err := s.repo.AddParticipant(ctx, p); err != nil {
			return err
		}
		if err := s.recordParticipant(ctx, "booking.participant_added", b, p.ID, nil, p); err != nil {
			return err
		}
		out = p
		return s.recomputeLocked(ctx, bookingID, &evs)
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, evs)
	return out, nil
}

func (s *Service) UpdateParticipant(ctx context.Context, bookingID, participantID uuid.UUID, in UpdateParticipantInput) (*domain.Participant, error) {
	name := strings.TrimSpace(in.FullName)
	if name == "" {
		return nil, shared.NewValidation("full_name is required")
	}
	var out *domain.Participant
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status.Terminal() {
			return shared.NewInvalidState("cannot update participants on a " + string(b.Status) + " booking")
		}
		parts, err := s.repo.ListParticipants(ctx, bookingID)
		if err != nil {
			return err
		}
		var found *domain.Participant
		for i := range parts {
			if parts[i].ID == participantID {
				found = &parts[i]
				break
			}
		}
		if found == nil {
			return shared.NewNotFound("participant")
		}
		prev := *found
		found.FullName = name
		if p, ok := shared.PassportUpdate(in.PassportNo); ok {
			found.PassportNo = p
		}
		found.Nationality = strings.TrimSpace(in.Nationality)
		found.DateOfBirth = in.DateOfBirth
		if err := s.repo.UpdateParticipant(ctx, found); err != nil {
			return err
		}
		if err := s.recordParticipant(ctx, "booking.participant_updated", b, found.ID, &prev, found); err != nil {
			return err
		}
		out = found
		return s.recomputeLocked(ctx, bookingID, &evs)
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, evs)
	return out, nil
}

func (s *Service) DeleteParticipant(ctx context.Context, bookingID, participantID uuid.UUID) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if !b.Status.Editable() {
			return shared.NewInvalidState("participants can only be removed from draft or quoted bookings")
		}
		var removed *domain.Participant
		parts, err := s.repo.ListParticipants(ctx, bookingID)
		if err != nil {
			return err
		}
		for i := range parts {
			if parts[i].ID == participantID {
				removed = &parts[i]
				break
			}
		}
		if removed == nil {
			return shared.NewNotFound("participant")
		}
		if err := s.repo.DeleteParticipant(ctx, bookingID, participantID); err != nil {
			return err
		}
		return s.recordParticipant(ctx, "booking.participant_removed", b, participantID, removed, nil)
	})
}

func (s *Service) ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]domain.Participant, error) {
	if _, err := s.repo.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return s.repo.ListParticipants(ctx, bookingID)
}

// normalizeLine accepts the legacy request shape where kind carried the
// category (package, hotel, ...) and treats it as an item line.
func normalizeLine(it LineItemInput) (kind, category string, err error) {
	kind, category = strings.TrimSpace(it.Kind), strings.TrimSpace(it.Category)
	if domain.ValidLineCategory(kind) && category == "" {
		kind, category = domain.KindItem, kind
	}
	if kind == "" {
		kind = domain.KindItem
	}
	if !domain.ValidLineKind(kind) {
		return "", "", shared.NewValidation("invalid line kind: " + kind + " (item|tax|fee)")
	}
	if kind == domain.KindItem && !domain.ValidLineCategory(category) {
		return "", "", shared.NewValidation("item lines need a category (package|hotel|room|transport|flight|extras)")
	}
	if kind != domain.KindItem && category != "" {
		return "", "", shared.NewValidation(kind + " lines cannot have a category")
	}
	return kind, category, nil
}

func (s *Service) SetLineItems(ctx context.Context, bookingID uuid.UUID, items []LineItemInput) (*domain.Booking, []domain.LineItem, error) {
	lines := make([]domain.LineItem, 0, len(items))
	now := s.now()
	for i, it := range items {
		kind, category, err := normalizeLine(it)
		if err != nil {
			return nil, nil, err
		}
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		label := strings.TrimSpace(it.Label)
		if label == "" {
			label = category
			if label == "" {
				label = kind
			}
		}
		if it.UnitPrice < 0 || it.UnitCost < 0 {
			return nil, nil, shared.NewValidation("unit_price and unit_cost must be >= 0")
		}
		lines = append(lines, domain.LineItem{
			ID: uuid.New(), BookingID: bookingID, Kind: kind, Category: category, Label: label,
			Quantity: qty, UnitPrice: it.UnitPrice, UnitCost: it.UnitCost, SortOrder: i,
			CreatedAt: now, UpdatedAt: now,
		})
	}

	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, bookingID)
		if err != nil {
			return err
		}
		if !b.Status.Editable() {
			return shared.NewInvalidState("only draft or quoted bookings can change line items")
		}
		prevLines, err := s.repo.ListLineItems(ctx, bookingID)
		if err != nil {
			return err
		}
		prevTotals := totalsSnapshot(b)
		if err := b.RecalculateFromLines(lines, now); err != nil {
			return err
		}
		if err := s.repo.ReplaceLineItems(ctx, bookingID, lines); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		if err := s.recordBooking(ctx, "booking.line_items_changed", b,
			map[string]any{"line_items": lineItemsSnapshot(prevLines), "totals": prevTotals},
			map[string]any{"line_items": lineItemsSnapshot(lines), "totals": totalsSnapshot(b)}, nil); err != nil {
			return err
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	stored, err := s.repo.ListLineItems(ctx, bookingID)
	if err != nil {
		return out, lines, nil
	}
	return out, stored, nil
}

func (s *Service) ListLineItems(ctx context.Context, bookingID uuid.UUID) ([]domain.LineItem, error) {
	if _, err := s.repo.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return s.repo.ListLineItems(ctx, bookingID)
}

func (s *Service) ListChecklist(ctx context.Context, bookingID uuid.UUID) ([]domain.ChecklistItem, error) {
	if _, err := s.repo.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	items, err := s.repo.ListChecklist(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		seed := domain.DefaultChecklist(bookingID)
		_ = s.repo.SeedChecklist(ctx, seed)
		return s.repo.ListChecklist(ctx, bookingID)
	}
	return items, nil
}

func (s *Service) UpdateChecklistItem(ctx context.Context, bookingID, itemID uuid.UUID, in ChecklistUpdateInput) (*domain.ChecklistItem, error) {
	var out *domain.ChecklistItem
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if _, err := s.repo.FindByID(ctx, bookingID); err != nil {
			return shared.NewNotFound("booking")
		}
		items, err := s.repo.ListChecklist(ctx, bookingID)
		if err != nil {
			return err
		}
		var found *domain.ChecklistItem
		for i := range items {
			if items[i].ID == itemID {
				found = &items[i]
				break
			}
		}
		if found == nil {
			return shared.NewNotFound("checklist item")
		}
		found.Completed = in.Completed
		if in.Completed {
			now := s.now()
			found.CompletedAt = &now
		} else {
			found.CompletedAt = nil
		}
		if err := s.repo.UpdateChecklistItem(ctx, found); err != nil {
			return err
		}
		out = found
		return s.recomputeLocked(ctx, bookingID, &evs)
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, evs)
	return out, nil
}

// readinessFacts gathers the readiness gate inputs; errors fail closed.
func (s *Service) readinessFacts(ctx context.Context, b *domain.Booking) (domain.ReadinessFacts, error) {
	var f domain.ReadinessFacts
	parts, err := s.repo.ListParticipants(ctx, b.ID)
	if err != nil {
		return f, err
	}
	f.ParticipantsMissing = len(parts) < b.PaxCount
	checklist, err := s.repo.ListChecklist(ctx, b.ID)
	if err != nil {
		return f, err
	}
	if len(checklist) == 0 {
		checklist = domain.DefaultChecklist(b.ID)
	}
	for _, c := range checklist {
		if c.Required && !c.Completed {
			f.ChecklistIncomplete++
		}
	}
	if s.docs != nil {
		missing, err := s.docs.MissingRequiredKinds(ctx, b.ID)
		if err != nil {
			return f, err
		}
		f.MissingDocs = missing
	}
	override, err := s.repo.FindReadinessOverride(ctx, b.ID)
	if err != nil {
		return f, err
	}
	f.OverrideActive = override != nil
	return f, nil
}

// Readiness evaluates the readiness gate, confirm guards and travel risk
// alerts (T-064/T-065, T-268).
func (s *Service) Readiness(ctx context.Context, bookingID uuid.UUID) (*domain.Readiness, error) {
	b, err := s.repo.FindByID(ctx, bookingID)
	if err != nil {
		return nil, shared.NewNotFound("booking")
	}
	parts, err := s.repo.ListParticipants(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	checklist, err := s.repo.ListChecklist(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if len(checklist) == 0 {
		checklist = domain.DefaultChecklist(bookingID)
	}
	facts, err := s.readinessFacts(ctx, b)
	if err != nil {
		return nil, err
	}

	r := &domain.Readiness{
		BookingID:         b.ID,
		PaxCount:          b.PaxCount,
		ParticipantsCount: len(parts),
		BalanceAmt:        b.BalanceAmt,
		ConfirmGuards:     []domain.Guard{},
		Blocking:          []string{},
		Warnings:          []string{},
		RiskAlerts:        []string{},
		MissingDocs:       []string{},
		ReadinessOK:       facts.OK(),
		OverrideActive:    facts.OverrideActive,
	}
	for _, p := range parts {
		if p.PassportMissing() {
			r.MissingPassports++
		}
	}
	for _, c := range checklist {
		if !c.Required {
			continue
		}
		r.ChecklistRequired++
		if c.Completed {
			r.ChecklistCompleted++
		}
	}
	r.ChecklistIncomplete = r.ChecklistRequired - r.ChecklistCompleted
	if len(facts.MissingDocs) > 0 {
		r.MissingDocs = facts.MissingDocs
	}

	if facts.ParticipantsMissing {
		r.Blocking = append(r.Blocking, "participants incomplete for pax_count")
	}
	if !facts.OverrideActive {
		if r.ChecklistIncomplete > 0 {
			r.Blocking = append(r.Blocking, "required checklist items incomplete")
		}
		if len(facts.MissingDocs) > 0 {
			r.Blocking = append(r.Blocking, "required documents not approved: "+strings.Join(facts.MissingDocs, ", "))
		}
	}

	dep, depErr := s.departures.FindDeparture(ctx, b.DepartureID)
	confirm := domain.TransitionInput{To: domain.StatusConfirmed, Actor: domain.ActorUser, Now: s.now()}
	if depErr == nil {
		sold, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
		if err != nil {
			return nil, err
		}
		confirm.Facts = departureFacts(dep, sold, b.PaxCount, s.now().In(s.loc))
		days := int(dep.DepartDate.Sub(s.now()).Hours() / 24)
		r.DaysToDeparture = &days
		if days >= 0 && days <= 14 {
			r.RiskAlerts = append(r.RiskAlerts, "departure within 14 days")
		}
		if days < 0 {
			r.RiskAlerts = append(r.RiskAlerts, "departure date has passed")
		}
	} else {
		r.Blocking = append(r.Blocking, "departure not found")
	}
	if domain.CanTransition(b.Status, domain.StatusConfirmed, domain.ActorUser) {
		r.ConfirmGuards = append(r.ConfirmGuards, b.Guards(confirm)...)
		r.CanConfirm = len(r.ConfirmGuards) == 0
	}

	if r.MissingPassports > 0 {
		r.Warnings = append(r.Warnings, "passport details missing for one or more participants")
		r.RiskAlerts = append(r.RiskAlerts, "missing passports")
	}
	if b.BalanceAmt > 0 {
		r.Warnings = append(r.Warnings, "outstanding balance remaining")
	}
	if b.Margin() < 0 {
		r.Warnings = append(r.Warnings, "negative margin after discount/cost")
		r.RiskAlerts = append(r.RiskAlerts, "negative margin")
	}
	if facts.OverrideActive {
		if o, _ := s.repo.FindReadinessOverride(ctx, bookingID); o != nil {
			r.Warnings = append(r.Warnings, "readiness override active: "+o.Reason)
		}
	}
	return r, nil
}

// OverrideReadiness lifts the checklist and document gates of readiness
// (T-271); the route requires bookings.override.
func (s *Service) OverrideReadiness(ctx context.Context, bookingID, actorID uuid.UUID, reason string) (*domain.ReadinessOverride, error) {
	reason = strings.TrimSpace(reason)
	if !domain.ValidOverrideReason(reason) {
		return nil, shared.NewValidation("reason must be at least 10 characters")
	}
	now := s.now()
	aid := actorID
	o := &domain.ReadinessOverride{
		ID: uuid.New(), BookingID: bookingID, Reason: reason, ActorID: &aid, CreatedAt: now,
	}
	var evs []events.Event
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, bookingID)
		if err != nil {
			return err
		}
		if b.Status.Terminal() {
			return shared.NewInvalidState("cannot override readiness of a " + string(b.Status) + " booking")
		}
		facts, err := s.readinessFacts(ctx, b)
		if err != nil {
			return err
		}
		var before any
		if prev, _ := s.repo.FindReadinessOverride(ctx, bookingID); prev != nil {
			before = map[string]any{"reason": prev.Reason, "actor_id": prev.ActorID, "created_at": prev.CreatedAt}
		}
		if err := s.repo.UpsertReadinessOverride(ctx, o); err != nil {
			return err
		}
		if err := s.recordBooking(ctx, "booking.readiness_overridden", b, before,
			map[string]any{"reason": o.Reason, "actor_id": o.ActorID, "created_at": o.CreatedAt},
			map[string]any{"checklist_incomplete": facts.ChecklistIncomplete, "missing_docs": facts.MissingDocs}); err != nil {
			return err
		}
		return s.rederive(ctx, b, nil, &evs)
	})
	if err != nil {
		return nil, err
	}
	s.publish(ctx, evs)
	return o, nil
}
