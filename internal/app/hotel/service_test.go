package hotel

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/hotel"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct {
	hotels     map[uuid.UUID]domain.Hotel
	seasons    map[uuid.UUID]domain.Season
	allotments map[uuid.UUID]domain.Allotment
	stops      map[uuid.UUID]domain.StopSale
}

func newMemRepo() *memRepo {
	return &memRepo{
		hotels: map[uuid.UUID]domain.Hotel{}, seasons: map[uuid.UUID]domain.Season{},
		allotments: map[uuid.UUID]domain.Allotment{}, stops: map[uuid.UUID]domain.StopSale{},
	}
}

func (m *memRepo) Create(_ context.Context, h *domain.Hotel) error { m.hotels[h.ID] = *h; return nil }
func (m *memRepo) Update(_ context.Context, h *domain.Hotel) error { m.hotels[h.ID] = *h; return nil }
func (m *memRepo) Find(_ context.Context, id uuid.UUID) (*domain.Hotel, error) {
	h, ok := m.hotels[id]
	if !ok {
		return nil, shared.NewNotFound("hotel")
	}
	return &h, nil
}
func (m *memRepo) FindForUpdate(ctx context.Context, id uuid.UUID) (*domain.Hotel, error) {
	return m.Find(ctx, id)
}
func (m *memRepo) List(context.Context, domain.ListFilter) ([]domain.Summary, error) { return nil, nil }
func (m *memRepo) ListSeasons(_ context.Context, hotelID uuid.UUID) ([]domain.Season, error) {
	out := []domain.Season{}
	for _, s := range m.seasons {
		if s.HotelID == hotelID {
			out = append(out, s)
		}
	}
	return out, nil
}
func (m *memRepo) SaveSeason(_ context.Context, s *domain.Season) error {
	m.seasons[s.ID] = *s
	return nil
}
func (m *memRepo) DeleteSeason(_ context.Context, _, id uuid.UUID) error {
	delete(m.seasons, id)
	return nil
}
func (m *memRepo) ListAllotments(_ context.Context, hotelID uuid.UUID) ([]domain.Allotment, error) {
	out := []domain.Allotment{}
	for _, a := range m.allotments {
		if a.HotelID == hotelID {
			out = append(out, a)
		}
	}
	return out, nil
}
func (m *memRepo) FindAllotment(_ context.Context, _, id uuid.UUID) (*domain.Allotment, error) {
	a, ok := m.allotments[id]
	if !ok {
		return nil, shared.NewNotFound("allotment")
	}
	return &a, nil
}
func (m *memRepo) SaveAllotment(_ context.Context, a *domain.Allotment) error {
	m.allotments[a.ID] = *a
	return nil
}
func (m *memRepo) DeleteAllotment(_ context.Context, _, id uuid.UUID) error {
	delete(m.allotments, id)
	return nil
}
func (m *memRepo) ListStopSales(context.Context, uuid.UUID) ([]domain.StopSale, error) {
	out := []domain.StopSale{}
	for _, s := range m.stops {
		out = append(out, s)
	}
	return out, nil
}
func (m *memRepo) CreateStopSale(_ context.Context, s *domain.StopSale) error {
	m.stops[s.ID] = *s
	return nil
}
func (m *memRepo) DeleteStopSale(_ context.Context, _, id uuid.UUID) error {
	delete(m.stops, id)
	return nil
}

type memAudit struct{ actions []string }

func (a *memAudit) Record(_ context.Context, in audit.RecordInput) error {
	a.actions = append(a.actions, in.Action)
	return nil
}

func fixture(t *testing.T) (*Service, *memAudit, uuid.UUID) {
	t.Helper()
	svc := NewService(newMemRepo(), tx.Nop{})
	rec := &memAudit{}
	svc.SetAuditor(rec)
	svc.now = func() time.Time { return time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC) }
	d, err := svc.Create(context.Background(), uuid.New(), HotelInput{
		Name: "Makkah Hilton Convention", Stars: 5, Currency: "SAR",
		Location:  domain.Location{City: "Makkah", Country: "SA", Landmark: domain.LandmarkHaram, DistanceM: 150},
		RoomTypes: []string{"standard", "quad"}, MealPlans: []string{"bb"},
		Markup: domain.Markup{Kind: domain.MarkupPercent, Value: 1000},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.Hotel.ChildPolicy.Child2MaxAge != 12 || d.Hotel.Cancellation.FreeDays != 14 {
		t.Fatalf("defaults not applied: %+v", d.Hotel)
	}
	return svc, rec, d.Hotel.ID
}

func day(s string) time.Time {
	t, _ := domain.ParseDay(s)
	return t
}

func TestSeasonsDoNotOverlap(t *testing.T) {
	svc, rec, id := fixture(t)
	ctx := context.Background()
	rates := []domain.Rate{{RoomType: "standard", MealPlan: "bb", Single: 800, Double: 400}}
	low, err := svc.SaveSeason(ctx, id, nil, SeasonInput{Name: "Low", Kind: "low", Start: day("2026-11-01"), End: day("2026-12-20"), Rates: rates})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.SaveSeason(ctx, id, nil, SeasonInput{Name: "Peak", Kind: "peak", Start: day("2026-12-20"), End: day("2027-01-05"), Rates: rates})
	var app *shared.AppError
	if !errors.As(err, &app) || app.Code != shared.NewConflict("").Code {
		t.Fatalf("overlap not rejected: %v", err)
	}
	if _, err := svc.SaveSeason(ctx, id, nil, SeasonInput{Name: "Peak", Kind: "peak", Start: day("2026-12-21"), End: day("2027-01-05"), Rates: rates}); err != nil {
		t.Fatal(err)
	}
	// Editing a season may keep its own dates.
	if _, err := svc.SaveSeason(ctx, id, &low.ID, SeasonInput{Name: "Low+", Kind: "low", Start: day("2026-11-01"), End: day("2026-12-20"), Rates: rates}); err != nil {
		t.Fatal(err)
	}
	if got := rec.actions[len(rec.actions)-1]; got != "hotel.season_updated" {
		t.Fatalf("audit %v", rec.actions)
	}
}

func TestOfferedRoomTypesStayConsistent(t *testing.T) {
	svc, _, id := fixture(t)
	ctx := context.Background()
	if _, err := svc.SaveAllotment(ctx, id, nil, AllotmentInput{RoomType: "quad", Kind: "guaranteed", Start: day("2026-12-01"), End: day("2026-12-10"), Rooms: 5, ReleaseDays: 7}); err != nil {
		t.Fatal(err)
	}
	d, _ := svc.Get(ctx, id)
	in := HotelInput{
		Name: d.Hotel.Name, Stars: 5, Currency: "SAR", Location: d.Hotel.Location,
		RoomTypes: []string{"standard"}, MealPlans: []string{"bb"}, Markup: d.Hotel.Markup,
	}
	if _, err := svc.Update(ctx, id, in); err == nil {
		t.Fatal("removed a room type still held in an allotment")
	}
	in.RoomTypes = []string{"standard", "quad", "suite"}
	if _, err := svc.Update(ctx, id, in); err != nil {
		t.Fatal(err)
	}
}

func TestAllotmentSalesAndRelease(t *testing.T) {
	svc, _, id := fixture(t)
	ctx := context.Background()
	a, err := svc.SaveAllotment(ctx, id, nil, AllotmentInput{RoomType: "standard", Kind: "guaranteed", Start: day("2026-12-21"), End: day("2026-12-31"), Rooms: 20, ReleaseDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	if a, err = svc.AdjustAllotment(ctx, id, a.ID, 8); err != nil || a.Sold != 8 {
		t.Fatalf("sell: %v %+v", err, a)
	}
	if _, err := svc.AdjustAllotment(ctx, id, a.ID, 13); err == nil {
		t.Fatal("oversold the block")
	}
	if err := svc.DeleteAllotment(ctx, id, a.ID); err == nil {
		t.Fatal("deleted a block with sold rooms")
	}
	edited, err := svc.SaveAllotment(ctx, id, &a.ID, AllotmentInput{RoomType: "standard", Kind: "guaranteed", Start: day("2026-12-21"), End: day("2026-12-31"), Rooms: 10, ReleaseDays: 7})
	if err != nil || edited.Sold != 8 {
		t.Fatalf("edit keeps sold rooms: %v %+v", err, edited)
	}
	if _, err := svc.SaveAllotment(ctx, id, &a.ID, AllotmentInput{RoomType: "standard", Kind: "guaranteed", Start: day("2026-12-21"), End: day("2026-12-31"), Rooms: 5}); err == nil {
		t.Fatal("shrunk the block below the sold rooms")
	}
	svc.now = func() time.Time { return time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC) }
	if _, err := svc.AdjustAllotment(ctx, id, a.ID, 1); err == nil {
		t.Fatal("sold from a released block")
	}
	if _, err := svc.AdjustAllotment(ctx, id, a.ID, -2); err != nil {
		t.Fatalf("returns stay possible after release: %v", err)
	}
}

func TestQuoteUsesBusinessToday(t *testing.T) {
	svc, _, id := fixture(t)
	ctx := context.Background()
	rates := []domain.Rate{{RoomType: "standard", MealPlan: "bb", Single: 800, Double: 400}}
	if _, err := svc.SaveSeason(ctx, id, nil, SeasonInput{Name: "Low", Kind: "low", Start: day("2026-11-01"), End: day("2026-12-20"), Rates: rates}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateStopSale(ctx, id, uuid.New(), StopSaleInput{Start: day("2026-12-05"), End: day("2026-12-06"), Reason: "full"}); err != nil {
		t.Fatal(err)
	}
	q, err := svc.Quote(ctx, id, domain.QuoteRequest{CheckIn: day("2026-12-01"), CheckOut: day("2026-12-03"), RoomType: "standard", MealPlan: "bb", Adults: 2})
	if err != nil {
		t.Fatal(err)
	}
	if q.NetTotal != 1600 || q.GrossTotal != 1760 || q.Availability != domain.AvailOnRequest {
		t.Fatalf("quote %+v", q)
	}
	q, _ = svc.Quote(ctx, id, domain.QuoteRequest{CheckIn: day("2026-12-04"), CheckOut: day("2026-12-06"), RoomType: "standard", MealPlan: "bb", Adults: 2})
	if q.Availability != domain.AvailStopSale {
		t.Fatalf("stop sale ignored: %s", q.Availability)
	}
}
