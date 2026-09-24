package booking

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

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
}

type UpdateInput struct {
	PaxCount    int
	TotalAmount int64
	Currency    string
	DiscountAmt *int64
	Notes       *string
}

type AddParticipantInput struct {
	FullName    string
	PassportNo  string
	Nationality string
	DateOfBirth *time.Time
}

type UpdateParticipantInput struct {
	FullName    string
	PassportNo  string
	Nationality string
	DateOfBirth *time.Time
}

type LineItemInput struct {
	Kind      string
	Label     string
	Quantity  int
	UnitPrice int64
	UnitCost  int64
}

type ChecklistUpdateInput struct {
	Completed bool
}

type ListInput struct {
	BranchID    uuid.UUID
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
	departures pkgdomain.Repository
	tx         tx.Runner
	bus        *events.Bus
	audit      audit.Recorder
	docs       DocReadiness
}

// DocReadiness evaluates policy-required documents for confirm gates (Epic 12).
type DocReadiness interface {
	MissingRequiredKinds(ctx context.Context, bookingID uuid.UUID) ([]string, error)
}

func NewService(
	repo domain.Repository,
	departures pkgdomain.Repository,
	txm tx.Runner,
	bus *events.Bus,
) *Service {
	return &Service{repo: repo, departures: departures, tx: txm, bus: bus}
}

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }
func (s *Service) SetDocReadiness(d DocReadiness) { s.docs = d }

func (s *Service) CreateDraft(ctx context.Context, in CreateInput) (*domain.Booking, error) {
	if in.PaxCount <= 0 {
		return nil, shared.NewValidation("pax_count must be > 0")
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
	now := time.Now().UTC()
	b := &domain.Booking{
		ID:          uuid.New(),
		BranchID:    in.BranchID,
		CustomerID:  in.CustomerID,
		DepartureID: in.DepartureID,
		LeadID:      in.LeadID,
		Status:      domain.StatusDraft,
		PaxCount:    in.PaxCount,
		TotalAmount: in.TotalAmount,
		DiscountAmt: in.DiscountAmt,
		Currency:    in.Currency,
		Notes:       strings.TrimSpace(in.Notes),
		OwnerID:     in.OwnerID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	b.RecomputeBalance()

	checklist := domain.DefaultChecklist(b.ID)
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, b); err != nil {
			return err
		}
		return s.repo.SeedChecklist(ctx, checklist)
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

func (s *Service) List(ctx context.Context, in ListInput) ([]domain.Booking, int, error) {
	f := domain.ListFilter{
		CustomerID: in.CustomerID, DepartureID: in.DepartureID, OwnerID: in.OwnerID,
		LeadID: in.LeadID, Status: in.Status, Query: in.Query, Limit: in.Limit, Offset: in.Offset,
	}
	if in.BranchID != uuid.Nil {
		bid := in.BranchID
		f.BranchID = &bid
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
		if err := b.ApplyUpdate(in.PaxCount, in.TotalAmount, in.Currency, in.DiscountAmt, in.Notes); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		out = b
		return nil
	})
	return out, err
}

func (s *Service) Confirm(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	ready, err := s.Readiness(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if !ready.CanConfirm {
		msg := "booking not ready to confirm"
		if len(ready.Blocking) > 0 {
			msg = strings.Join(ready.Blocking, "; ")
		}
		return nil, shared.NewInvalidState(msg)
	}

	var out *domain.Booking
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		dep, err := s.departures.FindDeparture(ctx, b.DepartureID)
		if err != nil {
			return shared.NewNotFound("departure")
		}
		soldBefore, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
		if err != nil {
			return err
		}
		check := *dep
		check.CapacitySold = soldBefore
		if !check.CanSell(b.PaxCount) {
			return shared.NewConflict("departure capacity exceeded")
		}
		if err := b.TransitionTo(domain.StatusConfirmed); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		soldAfter, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
		if err != nil {
			return err
		}
		if err := s.departures.UpdateDepartureCapacitySold(ctx, b.DepartureID, soldAfter); err != nil {
			return err
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.audit != nil && out != nil {
		id := out.ID
		_ = s.audit.Record(ctx, audit.RecordInput{
			ActorID: out.OwnerID, Action: "booking.status_changed", EntityType: "booking", EntityID: &id, BranchID: &out.BranchID,
			After: map[string]any{"status": out.Status},
		})
	}
	s.bus.Publish(ctx, events.Event{Name: events.BookingConfirmed, Payload: out})
	return out, nil
}

func (s *Service) Transition(ctx context.Context, bookingID uuid.UUID, to domain.Status) (*domain.Booking, error) {
	switch to {
	case domain.StatusConfirmed:
		return s.Confirm(ctx, bookingID)
	case domain.StatusCancelled:
		return s.Cancel(ctx, bookingID)
	case domain.StatusCompleted:
		return s.complete(ctx, bookingID)
	default:
		return nil, shared.NewValidation("unsupported status")
	}
}

func (s *Service) Cancel(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	var out *domain.Booking
	var wasConfirmed bool
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		wasConfirmed = b.Status == domain.StatusConfirmed
		if err := b.TransitionTo(domain.StatusCancelled); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
		}
		if wasConfirmed {
			sold, err := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
			if err != nil {
				return err
			}
			if err := s.departures.UpdateDepartureCapacitySold(ctx, b.DepartureID, sold); err != nil {
				return err
			}
		}
		out = b
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.audit != nil && out != nil {
		id := out.ID
		_ = s.audit.Record(ctx, audit.RecordInput{
			ActorID: out.OwnerID, Action: "booking.status_changed", EntityType: "booking", EntityID: &id, BranchID: &out.BranchID,
			After: map[string]any{"status": out.Status},
		})
	}
	s.bus.Publish(ctx, events.Event{Name: events.BookingCancelled, Payload: out})
	return out, nil
}

func (s *Service) complete(ctx context.Context, bookingID uuid.UUID) (*domain.Booking, error) {
	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if err := b.TransitionTo(domain.StatusCompleted); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, b); err != nil {
			return err
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
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status == domain.StatusCancelled {
			return shared.NewInvalidState("cannot add participants to cancelled booking")
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
			CreatedAt:   time.Now().UTC(),
		}
		if err := s.repo.AddParticipant(ctx, p); err != nil {
			return err
		}
		out = p
		return nil
	})
	return out, err
}

func (s *Service) UpdateParticipant(ctx context.Context, bookingID, participantID uuid.UUID, in UpdateParticipantInput) (*domain.Participant, error) {
	name := strings.TrimSpace(in.FullName)
	if name == "" {
		return nil, shared.NewValidation("full_name is required")
	}
	var out *domain.Participant
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status == domain.StatusCancelled {
			return shared.NewInvalidState("cannot update participants on cancelled booking")
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
		found.FullName = name
		found.PassportNo = strings.TrimSpace(in.PassportNo)
		found.Nationality = strings.TrimSpace(in.Nationality)
		found.DateOfBirth = in.DateOfBirth
		if err := s.repo.UpdateParticipant(ctx, found); err != nil {
			return err
		}
		out = found
		return nil
	})
	return out, err
}

func (s *Service) DeleteParticipant(ctx context.Context, bookingID, participantID uuid.UUID) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status != domain.StatusDraft {
			return shared.NewInvalidState("participants can only be removed from draft bookings")
		}
		return s.repo.DeleteParticipant(ctx, bookingID, participantID)
	})
}

func (s *Service) ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]domain.Participant, error) {
	if _, err := s.repo.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	return s.repo.ListParticipants(ctx, bookingID)
}

func (s *Service) SetLineItems(ctx context.Context, bookingID uuid.UUID, items []LineItemInput) (*domain.Booking, []domain.LineItem, error) {
	lines := make([]domain.LineItem, 0, len(items))
	now := time.Now().UTC()
	for i, it := range items {
		if !domain.ValidLineKind(it.Kind) {
			return nil, nil, shared.NewValidation("invalid line kind: " + it.Kind)
		}
		qty := it.Quantity
		if qty <= 0 {
			qty = 1
		}
		label := strings.TrimSpace(it.Label)
		if label == "" {
			label = it.Kind
		}
		if it.UnitPrice < 0 || it.UnitCost < 0 {
			return nil, nil, shared.NewValidation("unit_price and unit_cost must be >= 0")
		}
		lines = append(lines, domain.LineItem{
			ID: uuid.New(), BookingID: bookingID, Kind: it.Kind, Label: label,
			Quantity: qty, UnitPrice: it.UnitPrice, UnitCost: it.UnitCost, SortOrder: i,
			CreatedAt: now, UpdatedAt: now,
		})
	}

	var out *domain.Booking
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.repo.FindByID(ctx, bookingID)
		if err != nil {
			return shared.NewNotFound("booking")
		}
		if b.Status != domain.StatusDraft {
			return shared.NewInvalidState("only draft bookings can change line items")
		}
		if err := s.repo.ReplaceLineItems(ctx, bookingID, lines); err != nil {
			return err
		}
		b.RecalculateFromLines(lines)
		if err := s.repo.Update(ctx, b); err != nil {
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
			now := time.Now().UTC()
			found.CompletedAt = &now
		} else {
			found.CompletedAt = nil
		}
		if err := s.repo.UpdateChecklistItem(ctx, found); err != nil {
			return err
		}
		out = found
		return nil
	})
	return out, err
}

// Readiness evaluates confirm gates + travel risk alerts (T-064/T-065).
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

	r := &domain.Readiness{
		BookingID:         b.ID,
		PaxCount:          b.PaxCount,
		ParticipantsCount: len(parts),
		BalanceAmt:        b.BalanceAmt,
		Blocking:          []string{},
		Warnings:          []string{},
		RiskAlerts:        []string{},
		MissingDocs:       []string{},
	}

	missingPassports := 0
	for _, p := range parts {
		if p.PassportMissing() {
			missingPassports++
		}
	}
	r.MissingPassports = missingPassports

	req, done := 0, 0
	for _, c := range checklist {
		if !c.Required {
			continue
		}
		req++
		if c.Completed {
			done++
		}
	}
	r.ChecklistRequired = req
	r.ChecklistCompleted = done
	r.ChecklistIncomplete = req - done

	if b.Status != domain.StatusDraft {
		r.Blocking = append(r.Blocking, "booking is not in draft status")
	}
	if len(parts) < b.PaxCount {
		r.Blocking = append(r.Blocking, "participants incomplete for pax_count")
	}
	if r.ChecklistIncomplete > 0 {
		r.Blocking = append(r.Blocking, "required checklist items incomplete")
	}

	if s.docs != nil {
		missing, err := s.docs.MissingRequiredKinds(ctx, bookingID)
		if err == nil && len(missing) > 0 {
			r.MissingDocs = missing
			r.Blocking = append(r.Blocking, "required documents not approved: "+strings.Join(missing, ", "))
		}
	}

	override, _ := s.repo.FindReadinessOverride(ctx, bookingID)
	if override != nil {
		r.OverrideActive = true
	}

	dep, err := s.departures.FindDeparture(ctx, b.DepartureID)
	if err == nil {
		sold, _ := s.repo.CountConfirmedPaxByDeparture(ctx, b.DepartureID)
		check := *dep
		check.CapacitySold = sold
		if !check.CanSell(b.PaxCount) {
			r.Blocking = append(r.Blocking, "departure capacity exceeded or sales closed")
		}
		days := int(dep.DepartDate.Sub(time.Now().UTC()).Hours() / 24)
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

	if missingPassports > 0 {
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

	r.CanConfirm = len(r.Blocking) == 0
	if r.OverrideActive && b.Status == domain.StatusDraft {
		// Override lifts document (and soft) blocks but still requires draft + capacity sanity.
		filtered := make([]string, 0, len(r.Blocking))
		for _, msg := range r.Blocking {
			if strings.HasPrefix(msg, "required documents not approved") ||
				msg == "required checklist items incomplete" {
				continue
			}
			filtered = append(filtered, msg)
		}
		r.Blocking = filtered
		r.Warnings = append(r.Warnings, "readiness override active: "+override.Reason)
		r.CanConfirm = len(r.Blocking) == 0
	}
	return r, nil
}

// OverrideReadiness records an explicit confirm override for missing docs (Epic 12).
func (s *Service) OverrideReadiness(ctx context.Context, bookingID, actorID uuid.UUID, reason string) (*domain.ReadinessOverride, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, shared.NewValidation("reason is required")
	}
	if _, err := s.repo.FindByID(ctx, bookingID); err != nil {
		return nil, shared.NewNotFound("booking")
	}
	now := time.Now().UTC()
	aid := actorID
	o := &domain.ReadinessOverride{
		ID: uuid.New(), BookingID: bookingID, Reason: reason, ActorID: &aid, CreatedAt: now,
	}
	if err := s.repo.UpsertReadinessOverride(ctx, o); err != nil {
		return nil, err
	}
	return o, nil
}
