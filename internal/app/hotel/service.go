// Package hotel runs the hotels & contracting use cases.
package hotel

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/hotel"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// HotelInput is the full editable profile of a hotel.
type HotelInput struct {
	Name         string
	NameAr       string
	Stars        int
	Location     domain.Location
	Contact      domain.Contact
	RoomTypes    []string
	MealPlans    []string
	Currency     string
	Markup       domain.Markup
	ChildPolicy  *domain.ChildPolicy
	Cancellation *domain.CancellationPolicy
	Notes        string
	IsActive     *bool
}

type SeasonInput struct {
	Name   string
	Kind   string
	Start  time.Time
	End    time.Time
	Markup *domain.Markup
	Rates  []domain.Rate
}

type AllotmentInput struct {
	RoomType    string
	Kind        string
	Start       time.Time
	End         time.Time
	Rooms       int
	ReleaseDays int
	Notes       string
}

type StopSaleInput struct {
	Start    time.Time
	End      time.Time
	RoomType string
	Reason   string
}

type Service struct {
	repo  domain.Repository
	tx    tx.Runner
	audit audit.Recorder
	loc   *time.Location
	now   func() time.Time
}

func NewService(repo domain.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, tx: txm, loc: time.UTC, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

// SetTimeZone sets the business day used for "today" (releases, seasons).
func (s *Service) SetTimeZone(loc *time.Location) {
	if loc != nil {
		s.loc = loc
	}
}

func (s *Service) today() time.Time { return domain.Day(s.now(), s.loc) }

func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.Summary, error) {
	if f.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	f.Today = s.today()
	return s.repo.List(ctx, f)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Detail, error) {
	h, err := s.repo.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.detail(ctx, h)
}

func (s *Service) detail(ctx context.Context, h *domain.Hotel) (*domain.Detail, error) {
	seasons, err := s.repo.ListSeasons(ctx, h.ID)
	if err != nil {
		return nil, err
	}
	allotments, err := s.repo.ListAllotments(ctx, h.ID)
	if err != nil {
		return nil, err
	}
	stops, err := s.repo.ListStopSales(ctx, h.ID)
	if err != nil {
		return nil, err
	}
	return &domain.Detail{Hotel: *h, Seasons: seasons, Allotments: allotments, StopSales: stops}, nil
}

// Today is the business day the service evaluates releases against.
func (s *Service) Today() time.Time { return s.today() }

func (in HotelInput) apply(h *domain.Hotel) {
	h.Name, h.NameAr, h.Stars = in.Name, in.NameAr, in.Stars
	h.Location, h.Contact = in.Location, in.Contact
	h.RoomTypes, h.MealPlans, h.Currency = in.RoomTypes, in.MealPlans, in.Currency
	h.Markup, h.Notes = in.Markup, in.Notes
	if in.ChildPolicy != nil {
		h.ChildPolicy = *in.ChildPolicy
	}
	if in.Cancellation != nil {
		h.Cancellation = *in.Cancellation
	}
	if in.IsActive != nil {
		h.IsActive = *in.IsActive
	}
}

func (s *Service) Create(ctx context.Context, branchID uuid.UUID, in HotelInput) (*domain.Detail, error) {
	if branchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	now := s.now()
	h := &domain.Hotel{
		ID: uuid.New(), BranchID: branchID, IsActive: true, CreatedAt: now, UpdatedAt: now,
		ChildPolicy: domain.DefaultChildPolicy(), Cancellation: domain.DefaultCancellationPolicy(),
	}
	in.apply(h)
	if err := h.Normalize(); err != nil {
		return nil, err
	}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, h); err != nil {
			return err
		}
		return s.record(ctx, "hotel.created", h, nil, snapshot(h), nil)
	})
	if err != nil {
		return nil, err
	}
	return &domain.Detail{Hotel: *h, Seasons: []domain.Season{}, Allotments: []domain.Allotment{}, StopSales: []domain.StopSale{}}, nil
}

// Update replaces the profile. Room types and meal plans still used by a
// season rate or allotment cannot be removed.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in HotelInput) (*domain.Detail, error) {
	var out *domain.Detail
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		before := snapshot(h)
		in.apply(h)
		h.UpdatedAt = s.now()
		if err := h.Normalize(); err != nil {
			return err
		}
		d, err := s.detail(ctx, h)
		if err != nil {
			return err
		}
		if err := ensureStillOffered(h, d); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, h); err != nil {
			return err
		}
		out = d
		out.Hotel = *h
		return s.record(ctx, "hotel.updated", h, before, snapshot(h), nil)
	})
	return out, err
}

func ensureStillOffered(h *domain.Hotel, d *domain.Detail) error {
	for _, season := range d.Seasons {
		for _, r := range season.Rates {
			if !h.Supports(r.RoomType, r.MealPlan) {
				return inUse(r.RoomType+"/"+r.MealPlan, "season "+season.Name)
			}
		}
	}
	for _, a := range d.Allotments {
		if !slices.Contains(h.RoomTypes, a.RoomType) {
			return inUse(a.RoomType, "an allotment")
		}
	}
	for _, st := range d.StopSales {
		if st.RoomType != "" && !slices.Contains(h.RoomTypes, st.RoomType) {
			return inUse(st.RoomType, "a stop sale")
		}
	}
	return nil
}

func inUse(what, where string) error {
	e := shared.NewConflict(what + " is still used by " + where)
	e.Details = map[string]any{"room_types": what + " is still used by " + where}
	return e
}

// SaveSeason creates (id nil) or replaces a season; seasons of a hotel never overlap.
func (s *Service) SaveSeason(ctx context.Context, hotelID uuid.UUID, id *uuid.UUID, in SeasonInput) (*domain.Season, error) {
	var out *domain.Season
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, hotelID)
		if err != nil {
			return err
		}
		seasons, err := s.repo.ListSeasons(ctx, hotelID)
		if err != nil {
			return err
		}
		now := s.now()
		season := &domain.Season{ID: uuid.New(), HotelID: hotelID, CreatedAt: now}
		action := "hotel.season_created"
		var before map[string]any
		if id != nil {
			i := slices.IndexFunc(seasons, func(x domain.Season) bool { return x.ID == *id })
			if i < 0 {
				return shared.NewNotFound("season")
			}
			season = &seasons[i]
			before = seasonSnapshot(season)
			action = "hotel.season_updated"
		}
		season.Name, season.Kind, season.Start, season.End = in.Name, in.Kind, in.Start, in.End
		season.Markup, season.Rates, season.UpdatedAt = in.Markup, in.Rates, now
		if err := season.Normalize(h); err != nil {
			return err
		}
		if err := domain.EnsureNoOverlap(season, seasons); err != nil {
			return err
		}
		if err := s.repo.SaveSeason(ctx, season); err != nil {
			return err
		}
		out = season
		return s.record(ctx, action, h, before, seasonSnapshot(season), map[string]any{"season_id": season.ID})
	})
	return out, err
}

func (s *Service) DeleteSeason(ctx context.Context, hotelID, id uuid.UUID) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, hotelID)
		if err != nil {
			return err
		}
		if err := s.repo.DeleteSeason(ctx, hotelID, id); err != nil {
			return err
		}
		return s.record(ctx, "hotel.season_deleted", h, nil, nil, map[string]any{"season_id": id})
	})
}

// SaveAllotment creates (id nil) or edits a block; sold rooms are kept.
func (s *Service) SaveAllotment(ctx context.Context, hotelID uuid.UUID, id *uuid.UUID, in AllotmentInput) (*domain.Allotment, error) {
	var out *domain.Allotment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, hotelID)
		if err != nil {
			return err
		}
		now := s.now()
		a := &domain.Allotment{ID: uuid.New(), HotelID: hotelID, CreatedAt: now}
		action := "hotel.allotment_created"
		var before map[string]any
		if id != nil {
			if a, err = s.repo.FindAllotment(ctx, hotelID, *id); err != nil {
				return err
			}
			before = allotmentSnapshot(a)
			action = "hotel.allotment_updated"
		}
		a.RoomType, a.Kind, a.Start, a.End = in.RoomType, in.Kind, in.Start, in.End
		a.Rooms, a.ReleaseDays, a.Notes, a.UpdatedAt = in.Rooms, in.ReleaseDays, in.Notes, now
		if err := a.Normalize(h); err != nil {
			return err
		}
		if err := s.repo.SaveAllotment(ctx, a); err != nil {
			return err
		}
		out = a
		return s.record(ctx, action, h, before, allotmentSnapshot(a), map[string]any{"allotment_id": a.ID})
	})
	return out, err
}

// AdjustAllotment records sold (delta > 0) or returned (delta < 0) rooms.
func (s *Service) AdjustAllotment(ctx context.Context, hotelID, id uuid.UUID, delta int) (*domain.Allotment, error) {
	var out *domain.Allotment
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, hotelID)
		if err != nil {
			return err
		}
		a, err := s.repo.FindAllotment(ctx, hotelID, id)
		if err != nil {
			return err
		}
		if delta > 0 && a.Available(s.today()) < delta {
			return shared.NewInvalidState("not enough rooms left in the allotment")
		}
		prev := a.Sold
		if err := a.AdjustSold(delta, s.now()); err != nil {
			return err
		}
		if err := s.repo.SaveAllotment(ctx, a); err != nil {
			return err
		}
		out = a
		return s.record(ctx, "hotel.allotment_adjusted", h,
			map[string]any{"sold": prev}, map[string]any{"sold": a.Sold}, map[string]any{"allotment_id": a.ID, "delta": delta})
	})
	return out, err
}

func (s *Service) DeleteAllotment(ctx context.Context, hotelID, id uuid.UUID) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, hotelID)
		if err != nil {
			return err
		}
		a, err := s.repo.FindAllotment(ctx, hotelID, id)
		if err != nil {
			return err
		}
		if a.Sold > 0 && a.Status(s.today()) != domain.AllotExpired {
			return shared.NewInvalidState("return the sold rooms before deleting the allotment")
		}
		if err := s.repo.DeleteAllotment(ctx, hotelID, id); err != nil {
			return err
		}
		return s.record(ctx, "hotel.allotment_deleted", h, allotmentSnapshot(a), nil, map[string]any{"allotment_id": id})
	})
}

func (s *Service) CreateStopSale(ctx context.Context, hotelID, actor uuid.UUID, in StopSaleInput) (*domain.StopSale, error) {
	var out *domain.StopSale
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, hotelID)
		if err != nil {
			return err
		}
		st := &domain.StopSale{
			ID: uuid.New(), HotelID: hotelID, Start: in.Start, End: in.End,
			RoomType: in.RoomType, Reason: in.Reason, CreatedAt: s.now(),
		}
		if actor != uuid.Nil {
			st.CreatedBy = &actor
		}
		if err := st.Normalize(h); err != nil {
			return err
		}
		if err := s.repo.CreateStopSale(ctx, st); err != nil {
			return err
		}
		out = st
		return s.record(ctx, "hotel.stop_sale_created", h, nil, map[string]any{
			"start_date": domain.FormatDay(st.Start), "end_date": domain.FormatDay(st.End), "room_type": st.RoomType, "reason": st.Reason,
		}, map[string]any{"stop_sale_id": st.ID})
	})
	return out, err
}

func (s *Service) DeleteStopSale(ctx context.Context, hotelID, id uuid.UUID) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		h, err := s.repo.FindForUpdate(ctx, hotelID)
		if err != nil {
			return err
		}
		if err := s.repo.DeleteStopSale(ctx, hotelID, id); err != nil {
			return err
		}
		return s.record(ctx, "hotel.stop_sale_deleted", h, nil, nil, map[string]any{"stop_sale_id": id})
	})
}

// Quote prices a stay from the hotel's contract as of today.
func (s *Service) Quote(ctx context.Context, hotelID uuid.UUID, req domain.QuoteRequest) (*domain.Quote, error) {
	d, err := s.Get(ctx, hotelID)
	if err != nil {
		return nil, err
	}
	req.Today = s.today()
	return domain.Price(&d.Hotel, d.Seasons, d.Allotments, d.StopSales, req)
}

func (s *Service) record(ctx context.Context, action string, h *domain.Hotel, before, after, extra map[string]any) error {
	if s.audit == nil {
		return nil
	}
	id, branch := h.ID, h.BranchID
	if extra == nil {
		extra = map[string]any{}
	}
	extra["name"] = h.Name
	return s.audit.Record(ctx, audit.RecordInput{
		Action: action, EntityType: "hotel", EntityID: &id, BranchID: &branch,
		Before: before, After: after, Extra: extra,
	})
}

func snapshot(h *domain.Hotel) map[string]any {
	return map[string]any{
		"name": h.Name, "stars": h.Stars, "city": h.Location.City, "currency": h.Currency,
		"room_types": h.RoomTypes, "meal_plans": h.MealPlans, "markup": h.Markup,
		"child_policy": h.ChildPolicy, "cancellation": h.Cancellation, "is_active": h.IsActive,
		"reservations_email": h.Contact.ReservationsEmail,
	}
}

func seasonSnapshot(s *domain.Season) map[string]any {
	return map[string]any{
		"name": s.Name, "kind": s.Kind, "start_date": domain.FormatDay(s.Start), "end_date": domain.FormatDay(s.End),
		"markup": s.Markup, "rates": s.Rates,
	}
}

func allotmentSnapshot(a *domain.Allotment) map[string]any {
	return map[string]any{
		"room_type": a.RoomType, "kind": a.Kind, "start_date": domain.FormatDay(a.Start), "end_date": domain.FormatDay(a.End),
		"rooms": a.Rooms, "sold": a.Sold, "release_days": a.ReleaseDays,
	}
}
