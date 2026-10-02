package flight

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appflight "github.com/wodi-crm/wodi-crm-be/internal/app/flight"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
)

type stubFares struct{ got []domain.FareQuery }

func (s *stubFares) Fares(_ context.Context, q domain.FareQuery) ([]domain.Fare, error) {
	s.got = append(s.got, q)
	dep, _ := time.Parse(time.RFC3339, "2026-12-10T13:05:00+03:00")
	return []domain.Fare{{Origin: "IST", Destination: "DXB", OriginAirport: "SAW", DestinationAirport: "DXB",
		Airline: "PC", FlightNumber: "742", DepartureAt: dep, DurationMinutes: 255, Price: 182.5, Currency: q.Currency,
		Link: "/search/IST1012DXB1?t=x"}}, nil
}

type stubPlaces struct{}

func (stubPlaces) Places(context.Context, string, string) ([]domain.Place, error) {
	return []domain.Place{{Code: "SAW", Type: "airport", Name: "Sabiha", CityCode: "IST", CityName: "Istanbul", CountryCode: "TR"}}, nil
}

type stubAirlines struct{}

func (stubAirlines) AirlineName(context.Context, string) string { return "Pegasus" }

type envelope struct {
	Data  map[string]any `json:"data"`
	Error *struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func call(t *testing.T, h http.HandlerFunc, target string) (int, envelope, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, target, nil))
	var env envelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	return rec.Code, env, rec.Body.Bytes()
}

func TestSearchHandler(t *testing.T) {
	fares := &stubFares{}
	linker := domain.AviasalesLinker{BaseURL: "https://www.aviasales.com", Marker: "578591"}
	h := Handler{Svc: appflight.NewService(fares, stubPlaces{}, stubAirlines{}, linker, appflight.Options{Enabled: true})}

	code, env, raw := call(t, h.Search, "/v1/flights/search?origin=ist&destination=dxb&date=2026-12-10&time=14:00&adults=2&children=1&currency=eur&direct=1")
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, raw)
	}
	q := env.Data["query"].(map[string]any)
	if q["origin"] != "IST" || q["departure"] != "2026-12-10T14:00" || q["children"] != float64(1) || q["currency"] != "EUR" || q["direct"] != true {
		t.Fatalf("query echo: %v", q)
	}
	if len(fares.got) != 1 || !fares.got[0].DirectOnly || fares.got[0].Month != "2026-12" {
		t.Fatalf("provider query: %+v", fares.got)
	}
	offers := env.Data["offers"].([]any)
	o := offers[0].(map[string]any)
	if o["local_departure"] != "2026-12-10T13:05" || o["departure_at"] != "2026-12-10T13:05:00+03:00" ||
		o["gap_minutes"] != float64(-55) || o["airline_name"] != "Pegasus" || o["price"] != 182.5 || o["closest"] != true {
		t.Fatalf("offer: %v", o)
	}
	if o["booking_url"] != "https://www.aviasales.com/search/IST1012DXB21?marker=578591&t=x" {
		t.Fatalf("booking link with travellers and marker: %v", o["booking_url"])
	}
	if env.Data["search_url"] != "https://www.aviasales.com/search/IST1012DXB21?marker=578591" {
		t.Fatalf("provider search link: %v", env.Data["search_url"])
	}
	if env.Data["window_hours"] != float64(72) {
		t.Fatalf("window: %v", env.Data["window_hours"])
	}

	code, env, _ = call(t, h.Search, "/v1/flights/search?origin=IST&destination=DXB&date=10-12-2026&adults=x")
	if code != http.StatusBadRequest || env.Error.Details["departure"] == nil || env.Error.Details["adults"] == nil {
		t.Fatalf("bad input: %d %+v", code, env.Error)
	}

	off := Handler{Svc: appflight.NewService(fares, stubPlaces{}, stubAirlines{}, linker, appflight.Options{})}
	code, env, _ = call(t, off.Search, "/v1/flights/search?origin=IST&destination=DXB&date=2026-12-10")
	if code != http.StatusServiceUnavailable || env.Error.Code != "flights_not_configured" ||
		env.Error.Details["search_url"] != "https://www.aviasales.com/search/IST1012DXB1?marker=578591" {
		t.Fatalf("not configured still links to the provider search: %d %+v", code, env.Error)
	}
}

func TestPlacesHandler(t *testing.T) {
	h := Handler{Svc: appflight.NewService(&stubFares{}, stubPlaces{}, stubAirlines{}, domain.AviasalesLinker{}, appflight.Options{})}
	rec := httptest.NewRecorder()
	h.Places(rec, httptest.NewRequest(http.MethodGet, "/v1/flights/places?term=sab&locale=en", nil))
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(env.Data) != 1 || env.Data[0]["code"] != "SAW" || env.Data[0]["city_name"] != "Istanbul" {
		t.Fatalf("places: %v", env.Data)
	}
}
