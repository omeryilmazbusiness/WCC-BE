package flight

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type fakeFares struct {
	mu    sync.Mutex
	calls []domain.FareQuery
	fail  map[bool]error // by DirectOnly
	fares map[string][]domain.Fare
}

func (f *fakeFares) Fares(_ context.Context, q domain.FareQuery) ([]domain.Fare, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, q)
	if err := f.fail[q.DirectOnly]; err != nil {
		return nil, err
	}
	return f.fares[q.Month], nil
}

type fakePlaces struct {
	calls int
	err   error
}

func (p *fakePlaces) Places(_ context.Context, term, locale string) ([]domain.Place, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	out := make([]domain.Place, 10)
	for i := range out {
		out[i] = domain.Place{Code: "IST", Name: term + "/" + locale}
	}
	return out, nil
}

type fakeAirlines map[string]string

func (a fakeAirlines) AirlineName(_ context.Context, code string) string { return a[code] }

type fakeLinker struct{}

func (fakeLinker) BookingURL(f domain.Fare, p domain.Passengers) string {
	return "https://book/" + f.Airline + "?adults=" + string(rune('0'+p.Adults))
}

func (fakeLinker) SearchURL(q domain.Query) string { return "https://search/" + q.Origin + q.Destination }

func dep(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func newSvc(fares *fakeFares, places *fakePlaces, enabled bool) (*Service, *time.Time) {
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)
	s := NewService(fares, places, fakeAirlines{"PC": "Pegasus"}, fakeLinker{}, Options{Enabled: enabled})
	s.now = func() time.Time { return now }
	return s, &now
}

func query() domain.Query {
	return domain.Query{Origin: "ist", Destination: "dxb", Departure: time.Date(2026, 10, 30, 14, 0, 0, 0, time.UTC),
		Passengers: domain.Passengers{Adults: 2}}
}

func TestSearch(t *testing.T) {
	fares := &fakeFares{fares: map[string][]domain.Fare{
		"2026-10": {{Airline: "PC", FlightNumber: "1", DepartureAt: dep("2026-10-30T13:00:00+03:00"), Price: 200}},
		"2026-11": {
			{Airline: "TK", FlightNumber: "2", DepartureAt: dep("2026-11-01T14:00:00+03:00"), Price: 150},
			{Airline: "TK", FlightNumber: "9", DepartureAt: dep("2026-11-20T14:00:00+03:00"), Price: 10},
		},
	}}
	svc, now := newSvc(fares, &fakePlaces{}, true)
	res, err := svc.Search(context.Background(), query())
	if err != nil {
		t.Fatal(err)
	}
	if len(fares.calls) != 4 {
		t.Fatalf("two months x (non-stop, any): %+v", fares.calls)
	}
	if res.Query.Origin != "IST" || res.Query.Currency != "USD" || res.Window != domain.SearchWindow || !res.FetchedAt.Equal(*now) ||
		res.SearchURL != "https://search/ISTDXB" {
		t.Fatalf("result meta: %+v", res)
	}
	if len(res.Offers) != 2 || res.Offers[0].Airline != "PC" || res.Offers[1].Airline != "TK" {
		t.Fatalf("deduped (both passes return the same fares), windowed, ranked: %+v", res.Offers)
	}
	o := res.Offers[0]
	if o.AirlineName != "Pegasus" || o.BookingURL != "https://book/PC?adults=2" || !o.Closest || o.GapMinutes != -60 {
		t.Fatalf("offer enrichment: %+v", o)
	}
	if !res.Offers[1].Cheapest || res.Offers[1].AirlineName != "" {
		t.Fatalf("cheapest badge / unknown airline: %+v", res.Offers[1])
	}

	if _, err := svc.Search(context.Background(), query()); err != nil || len(fares.calls) != 4 {
		t.Fatalf("second search served from cache: calls=%d err=%v", len(fares.calls), err)
	}
	*now = now.Add(fareTTL + time.Second)
	_, _ = svc.Search(context.Background(), query())
	if len(fares.calls) != 8 {
		t.Fatalf("cache expires: calls=%d", len(fares.calls))
	}

	direct := query()
	direct.DirectOnly = true
	direct.Departure = time.Date(2026, 12, 15, 9, 0, 0, 0, time.UTC)
	before := len(fares.calls)
	_, _ = svc.Search(context.Background(), direct)
	if got := fares.calls[before:]; len(got) != 1 || !got[0].DirectOnly {
		t.Fatalf("non-stop only asks once per month: %+v", got)
	}
}

func TestSearchFailures(t *testing.T) {
	ctx := context.Background()

	svc, _ := newSvc(&fakeFares{}, &fakePlaces{}, false)
	var app *shared.AppError
	if _, err := svc.Search(ctx, query()); !errors.As(err, &app) || app.Code != "flights_not_configured" || !errors.Is(err, shared.ErrUnavailable) ||
		app.Details["search_url"] != "https://search/ISTDXB" {
		t.Fatalf("disabled still links to the provider search: %v %v", err, app)
	}

	bad := query()
	bad.Destination = "IST"
	svc, _ = newSvc(&fakeFares{}, &fakePlaces{}, true)
	if _, err := svc.Search(ctx, bad); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("validation first: %v", err)
	}

	partial := &fakeFares{fail: map[bool]error{false: errors.New("boom")}, fares: map[string][]domain.Fare{
		"2026-10": {{Airline: "PC", FlightNumber: "1", DepartureAt: dep("2026-10-30T13:00:00+03:00"), Price: 200}},
	}}
	svc, _ = newSvc(partial, &fakePlaces{}, true)
	if res, err := svc.Search(ctx, query()); err != nil || len(res.Offers) != 1 {
		t.Fatalf("partial failure still answers: %v %+v", err, res)
	}

	down := &fakeFares{fail: map[bool]error{false: errors.New("boom"), true: errors.New("boom")}}
	svc, _ = newSvc(down, &fakePlaces{}, true)
	if _, err := svc.Search(ctx, query()); !errors.As(err, &app) || app.Code != "flights_unavailable" || app.RetryAfter == 0 || strings.Contains(app.Message, "boom") ||
		app.Details["search_url"] != "https://search/ISTDXB" {
		t.Fatalf("all failed: %v", err)
	}

	rejected := &fakeFares{fail: map[bool]error{false: domain.ErrProviderRejected, true: domain.ErrProviderRejected}}
	svc, _ = newSvc(rejected, &fakePlaces{}, true)
	if _, err := svc.Search(ctx, query()); !errors.As(err, &app) || app.Code != "flights_not_configured" {
		t.Fatalf("rejected token: %v", err)
	}
}

func TestPlaces(t *testing.T) {
	ctx := context.Background()
	places := &fakePlaces{}
	svc, _ := newSvc(&fakeFares{}, places, false)

	for _, term := range []string{"", " i ", strings.Repeat("x", 41)} {
		if got, err := svc.Places(ctx, term, "en"); err != nil || len(got) != 0 {
			t.Fatalf("%q: %v %v", term, got, err)
		}
	}
	if places.calls != 0 {
		t.Fatal("short/long terms never reach the provider")
	}
	got, err := svc.Places(ctx, " Ist ", "tr")
	if err != nil || len(got) != placeLimit || got[0].Name != "Ist/en" {
		t.Fatalf("works without a token, capped, unsupported locale falls back: %v %+v", err, got)
	}
	if _, _ = svc.Places(ctx, "ist", "en"); places.calls != 1 {
		t.Fatalf("case-insensitive cache: calls=%d", places.calls)
	}
	if got, _ := svc.Places(ctx, "دمشق", "ar"); got[0].Name != "دمشق/ar" {
		t.Fatalf("arabic: %+v", got)
	}

	places.err = errors.New("down")
	var app *shared.AppError
	if _, err := svc.Places(ctx, "dub", "en"); !errors.As(err, &app) || app.Code != "flights_unavailable" {
		t.Fatalf("provider down: %v", err)
	}
}

func TestTTLCacheBound(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := newTTLCache[int](time.Minute, 2, func() time.Time { return now })
	c.put("a", 1)
	now = now.Add(time.Second)
	c.put("b", 2)
	now = now.Add(time.Second)
	c.put("c", 3)
	if _, ok := c.get("a"); ok {
		t.Fatal("oldest evicted when full")
	}
	if v, ok := c.get("c"); !ok || v != 3 || len(c.entries) != 2 {
		t.Fatalf("bounded: %v %v %d", v, ok, len(c.entries))
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.get("b"); ok {
		t.Fatal("expired")
	}
}
