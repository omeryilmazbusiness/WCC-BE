package hotel

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func day(s string) time.Time {
	t, err := ParseDay(s)
	if err != nil {
		panic(err)
	}
	return t
}

func testHotel() *Hotel {
	h := &Hotel{
		ID: uuid.New(), Name: "Makkah Hilton Convention", Stars: 5, Currency: "sar",
		Location:     Location{City: "Makkah", Country: "sa", Landmark: LandmarkHaram, DistanceM: 150},
		RoomTypes:    []string{"quad", "standard", "deluxe", "nope"},
		MealPlans:    []string{"BB", "ro", "bb"},
		Markup:       Markup{Kind: MarkupPercent, Value: 1500},
		ChildPolicy:  DefaultChildPolicy(),
		Cancellation: DefaultCancellationPolicy(),
		IsActive:     true,
	}
	if err := h.Normalize(); err != nil {
		panic(err)
	}
	return h
}

func TestHotelNormalize(t *testing.T) {
	h := testHotel()
	if h.Currency != "SAR" || h.Location.Country != "SA" {
		t.Fatalf("not upper-cased: %s %s", h.Currency, h.Location.Country)
	}
	if got := h.RoomTypes; len(got) != 3 || got[0] != RoomStandard || got[2] != RoomQuad {
		t.Fatalf("room types not cleaned and ordered: %v", got)
	}
	if got := h.MealPlans; len(got) != 2 || got[0] != MealRO {
		t.Fatalf("meal plans not cleaned: %v", got)
	}

	bad := &Hotel{Name: "X", Currency: "riyal", Stars: 7, Location: Location{DistanceM: -1, Latitude: ptr(10.0)},
		Contact: Contact{SalesEmail: "nope", SalesPhone: "abc"}}
	err := bad.Normalize()
	var app *shared.AppError
	if !errors.As(err, &app) {
		t.Fatalf("want validation error, got %v", err)
	}
	for _, k := range []string{"name", "currency", "stars", "city", "distance_m", "latitude", "sales_email", "sales_phone", "room_types", "meal_plans"} {
		if _, ok := app.Details[k]; !ok {
			t.Errorf("missing field error %q in %v", k, app.Details)
		}
	}
}

func TestMarkupApply(t *testing.T) {
	if got := (Markup{Kind: MarkupPercent, Value: 1500}).Apply(10000, 2); got != 11500 {
		t.Fatalf("percent: %d", got)
	}
	if got := (Markup{Kind: MarkupFixed, Value: 2500}).Apply(10000, 2); got != 15000 {
		t.Fatalf("fixed per guest: %d", got)
	}
	if got := (Markup{Kind: MarkupFixed, Value: 2500}).Apply(0, 2); got != 0 {
		t.Fatalf("free stays free: %d", got)
	}
	m := Markup{Kind: MarkupPercent, Value: MaxMarkupBps + 1}
	if m.Normalize() == nil {
		t.Fatal("over-cap markup accepted")
	}
}

func TestChildPolicy(t *testing.T) {
	p := DefaultChildPolicy()
	p.Child1 = ChildRule{Mode: ChildPercent, Value: 50}
	p.Infant = ChildRule{Mode: ChildFixed, Value: 1000}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		age  int
		bed  bool
		band string
		net  int64
	}{
		{1, false, BandInfant, 1000},
		{2, false, BandChild1, 5000},
		{5, true, BandChild1, 5000},
		{6, true, BandChild2, 7500},
		{11, false, BandChild2, 5000},
	}
	for _, c := range cases {
		band, net, ok := p.ChildCharge(c.age, c.bed, 10000)
		if !ok || band != c.band || net != c.net {
			t.Errorf("age %d bed %v: got %s %d %v", c.age, c.bed, band, net, ok)
		}
	}
	if _, _, ok := p.ChildCharge(12, false, 10000); ok {
		t.Error("12 should be priced as adult")
	}
	bad := DefaultChildPolicy()
	bad.Infant = ChildRule{Mode: ChildPercent, Value: 10}
	if bad.Normalize() == nil {
		t.Error("infant percent accepted")
	}
	bad = DefaultChildPolicy()
	bad.Child1MaxAge = 1
	if bad.Normalize() == nil {
		t.Error("falling age breaks accepted")
	}
}

func TestCancellationPolicy(t *testing.T) {
	p := CancellationPolicy{FreeDays: 14, NoShowPct: 100, Tiers: []PenaltyTier{
		{MinDays: 0, Kind: "PERCENT", Value: 100}, {MinDays: 7, Kind: PenaltyNights, Value: 1},
	}}
	if err := p.Normalize(); err != nil {
		t.Fatal(err)
	}
	if p.Tiers[0].MinDays != 7 {
		t.Fatalf("tiers not sorted: %+v", p.Tiers)
	}
	nightly := []int64{1000, 1200, 1500}
	for _, c := range []struct {
		days int
		want int64
	}{{30, 0}, {14, 0}, {13, 1000}, {7, 1000}, {6, 3700}, {0, 3700}} {
		if got := p.Penalty(c.days, nightly); got != c.want {
			t.Errorf("%d days: got %d want %d", c.days, got, c.want)
		}
	}
	if p.NoShowPenalty(nightly) != 3700 {
		t.Error("no-show")
	}
	if got := FormatDay(p.FreeUntil(day("2026-12-21"))); got != "2026-12-07" {
		t.Errorf("free until %s", got)
	}
	bad := CancellationPolicy{FreeDays: 5, Tiers: []PenaltyTier{{MinDays: 5, Kind: PenaltyNights, Value: 1}}}
	if bad.Normalize() == nil {
		t.Error("tier at free_days accepted")
	}
	gap := CancellationPolicy{FreeDays: 10, Tiers: []PenaltyTier{{MinDays: 5, Kind: PenaltyNights, Value: 1}}}
	if err := gap.Normalize(); err != nil {
		t.Fatal(err)
	}
	if got := gap.Penalty(2, nightly); got != 3700 {
		t.Errorf("below the last tier the stay is due, got %d", got)
	}
}

func TestSeasonValidationAndOverlap(t *testing.T) {
	h := testHotel()
	s := Season{ID: uuid.New(), Name: "Peak", Kind: "PEAK", Start: day("2026-12-21"), End: day("2027-01-05"),
		Rates: []Rate{{RoomType: "deluxe", MealPlan: "bb", Double: 500}, {RoomType: "standard", MealPlan: "bb", Single: 800, Double: 450}}}
	if err := s.Normalize(h); err != nil {
		t.Fatal(err)
	}
	if s.Rates[0].RoomType != RoomStandard {
		t.Fatal("rates not ordered by room type")
	}
	bad := Season{Name: "X", Kind: "low", Start: day("2026-11-10"), End: day("2026-11-01"),
		Rates: []Rate{{RoomType: "suite", MealPlan: "bb", Double: 1}, {RoomType: "standard", MealPlan: "bb"}}}
	if bad.Normalize(h) == nil {
		t.Fatal("bad season accepted")
	}
	low := Season{ID: uuid.New(), Name: "Low", Start: day("2026-11-01"), End: day("2026-12-21")}
	if err := EnsureNoOverlap(&low, []Season{s}); err == nil {
		t.Fatal("touching ranges must overlap (inclusive end)")
	}
	low.End = day("2026-12-20")
	if err := EnsureNoOverlap(&low, []Season{s}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureNoOverlap(&s, []Season{s}); err != nil {
		t.Fatal("a season never overlaps itself")
	}
}

func TestAllotmentLifecycle(t *testing.T) {
	h := testHotel()
	a := Allotment{RoomType: "standard", Kind: AllotGuaranteed, Start: day("2026-12-21"), End: day("2026-12-31"), Rooms: 20, ReleaseDays: 7}
	if err := a.Normalize(h); err != nil {
		t.Fatal(err)
	}
	if got := FormatDay(a.ReleaseDate()); got != "2026-12-14" {
		t.Fatalf("release date %s", got)
	}
	if err := a.AdjustSold(5, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a.Available(day("2026-12-01")) != 15 || a.Status(day("2026-12-01")) != AllotOpen {
		t.Fatal("open block")
	}
	if a.Available(day("2026-12-14")) != 0 || a.Status(day("2026-12-14")) != AllotReleased {
		t.Fatal("unsold rooms go back on the release day")
	}
	if a.Status(day("2027-01-01")) != AllotExpired {
		t.Fatal("expired")
	}
	if a.AdjustSold(16, time.Now()) == nil || a.AdjustSold(-6, time.Now()) == nil {
		t.Fatal("bounds not enforced")
	}
	req := Allotment{RoomType: "standard", Kind: AllotOnRequest, Start: day("2026-12-21"), End: day("2026-12-31"), Rooms: 5, ReleaseDays: 9}
	if err := req.Normalize(h); err != nil || req.ReleaseDays != 0 {
		t.Fatalf("on-request blocks have no release: %v %d", err, req.ReleaseDays)
	}
	if req.Status(day("2026-12-20")) != AllotOpen {
		t.Fatal("on-request block never releases")
	}
}

func quoteFixture() (*Hotel, []Season, []Allotment, []StopSale) {
	h := testHotel()
	h.ChildPolicy.ExtraBedAdult = 200
	low := Season{ID: uuid.New(), Name: "Low", Kind: SeasonLow, Start: day("2026-11-01"), End: day("2026-12-20"),
		Rates: []Rate{{RoomType: "standard", MealPlan: "bb", Single: 800, Double: 400, Triple: 350, Quad: 300}}}
	peak := Season{ID: uuid.New(), Name: "Peak", Kind: SeasonPeak, Start: day("2026-12-21"), End: day("2027-01-05"),
		Markup: &Markup{Kind: MarkupFixed, Value: 100},
		Rates:  []Rate{{RoomType: "standard", MealPlan: "bb", Single: 1600, Double: 1000}}}
	for _, s := range []*Season{&low, &peak} {
		if err := s.Normalize(h); err != nil {
			panic(err)
		}
	}
	allot := []Allotment{{RoomType: "standard", Kind: AllotGuaranteed, Start: day("2026-12-01"), End: day("2026-12-31"), Rooms: 3, ReleaseDays: 7}}
	return h, []Season{low, peak}, allot, nil
}

func TestPriceAcrossSeasons(t *testing.T) {
	h, seasons, allot, _ := quoteFixture()
	q, err := Price(h, seasons, allot, nil, QuoteRequest{
		CheckIn: day("2026-12-19"), CheckOut: day("2026-12-22"), RoomType: "standard", MealPlan: "bb",
		Rooms: 2, Adults: 2, Children: []Child{{Age: 4}, {Age: 13}}, Today: day("2026-11-01"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// The 13-year-old prices as an adult: 3 adults, triple in low season, no triple in peak.
	if q.Adults != 3 || q.Nights != 3 {
		t.Fatalf("adults %d nights %d", q.Adults, q.Nights)
	}
	if q.Bookable || q.Availability != AvailUnavailable || len(q.MissingDates) != 1 || q.MissingDates[0] != "2026-12-21" {
		t.Fatalf("peak has no triple: %+v", q)
	}
	// Low nights: (350*3 + child1 free) * 2 rooms = 2100 net; gross +15%.
	if n := q.NightsDetail[0]; n.Net != 2100 || n.Gross != 2416 || n.SeasonKind != SeasonLow {
		t.Fatalf("low night %+v", n)
	}
	if len(q.Children) != 1 || q.Children[0].Band != BandChild1 {
		t.Fatalf("children lines %+v", q.Children)
	}
}

func TestPriceDoubleWithMarkupOverrideAndAvailability(t *testing.T) {
	h, seasons, allot, _ := quoteFixture()
	req := QuoteRequest{CheckIn: day("2026-12-21"), CheckOut: day("2026-12-23"), RoomType: "standard", MealPlan: "bb",
		Rooms: 2, Adults: 2, ExtraBed: true, Today: day("2026-11-01")}
	q, err := Price(h, seasons, allot, nil, req)
	if err != nil {
		t.Fatal(err)
	}
	// Per room night: 1000*2 + extra bed 200 = 2200 net; fixed markup 100 x 3 paying guests.
	if q.NetTotal != 2200*2*2 || q.GrossTotal != 2500*2*2 || q.Profit != 1200 {
		t.Fatalf("totals %d %d %d", q.NetTotal, q.GrossTotal, q.Profit)
	}
	if q.Availability != AvailInstant || q.AllotmentLeft != 3 || q.Guests != 6 {
		t.Fatalf("availability %s left %d guests %d", q.Availability, q.AllotmentLeft, q.Guests)
	}
	if q.Cancellation.FreeUntil != "2026-12-07" || !q.Cancellation.FreeNow || q.Cancellation.NoShow != q.GrossTotal {
		t.Fatalf("cancellation %+v", q.Cancellation)
	}
	if tier := q.Cancellation.Tiers[0]; tier.Amount != 5000 || tier.From != "2026-12-08" {
		t.Fatalf("one-night tier %+v", tier)
	}

	req.Rooms = 4
	q, _ = Price(h, seasons, allot, nil, req)
	if q.Availability != AvailOnRequest {
		t.Fatalf("beyond the allotment it is on request, got %s", q.Availability)
	}

	req.Rooms = 1
	req.Today = day("2026-12-15")
	q, _ = Price(h, seasons, allot, nil, req)
	if q.Availability != AvailOnRequest || q.AllotmentLeft != 0 {
		t.Fatalf("released block: %s %d", q.Availability, q.AllotmentLeft)
	}
	if q.Cancellation.FreeNow || q.Cancellation.PenaltyToday != 5000 {
		t.Fatalf("6 days out the whole stay is due: %+v", q.Cancellation)
	}

	stops := []StopSale{{Start: day("2026-12-22"), End: day("2026-12-22")}}
	q, _ = Price(h, seasons, allot, stops, req)
	if q.Availability != AvailStopSale || q.Bookable || len(q.StopSaleDates) != 1 {
		t.Fatalf("stop sale %+v", q)
	}
}

func TestPriceRejectsBadRequests(t *testing.T) {
	h, seasons, allot, _ := quoteFixture()
	base := QuoteRequest{CheckIn: day("2026-12-21"), CheckOut: day("2026-12-21"), RoomType: "standard", MealPlan: "bb", Adults: 2}
	if _, err := Price(h, seasons, allot, nil, base); err == nil {
		t.Fatal("zero nights accepted")
	}
	base.CheckOut = day("2026-12-22")
	base.MealPlan = "ai"
	if _, err := Price(h, seasons, allot, nil, base); err == nil {
		t.Fatal("unsupported meal plan accepted")
	}
	base.MealPlan = "bb"
	base.Adults = 4
	base.Children = []Child{{Age: 15}}
	if _, err := Price(h, seasons, allot, nil, base); err == nil {
		t.Fatal("five adults accepted")
	}
	h.IsActive = false
	base.Children = nil
	if _, err := Price(h, seasons, allot, nil, base); err == nil {
		t.Fatal("inactive hotel quoted")
	}
}

func ptr[T any](v T) *T { return &v }
