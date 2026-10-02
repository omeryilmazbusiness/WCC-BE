package travelpayouts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
)

func TestFares(t *testing.T) {
	var gotToken, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/aviasales/v3/prices_for_dates" {
			http.NotFound(w, r)
			return
		}
		gotToken, gotQuery = r.Header.Get("X-Access-Token"), r.URL.RawQuery
		_, _ = w.Write([]byte(`{"success":true,"currency":"usd","data":[
			{"origin":"IST","destination":"DXB","origin_airport":"SAW","destination_airport":"DXB","price":182,
			 "airline":"pc","flight_number":"742","departure_at":"2026-10-10T13:05:00+03:00","transfers":0,
			 "duration":255,"duration_to":255,"link":"/search/IST1010DXB1?t=PC1"},
			{"origin":"IST","destination":"DXB","price":150,"airline":"FZ","flight_number":1752,
			 "departure_at":"2026-10-11T02:30:00+03:00","transfers":1,"duration":400,"link":"/search/IST1110DXB1?t=FZ1"},
			{"origin":"IST","destination":"DXB","price":99,"airline":"XX","departure_at":"not-a-date"}
		]}`))
	}))
	defer srv.Close()

	f := NewFares(Config{APIURL: srv.URL, Token: "secret", Market: "tr"})
	fares, err := f.Fares(context.Background(), domain.FareQuery{
		Origin: "IST", Destination: "DXB", Month: "2026-10", Currency: "USD", DirectOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != "secret" || strings.Contains(gotQuery, "secret") {
		t.Fatalf("token must travel in the header only: header=%q query=%q", gotToken, gotQuery)
	}
	for _, want := range []string{"origin=IST", "destination=DXB", "departure_at=2026-10", "one_way=true", "direct=true", "currency=usd", "market=tr"} {
		if !strings.Contains(gotQuery, want) {
			t.Errorf("query lacks %s: %s", want, gotQuery)
		}
	}
	if len(fares) != 2 {
		t.Fatalf("unparseable rows are skipped: %+v", fares)
	}
	a, b := fares[0], fares[1]
	if a.Airline != "PC" || a.FlightNumber != "742" || a.OriginAirport != "SAW" || a.Currency != "USD" ||
		a.DurationMinutes != 255 || a.Link != "/search/IST1010DXB1?t=PC1" {
		t.Fatalf("mapped fare: %+v", a)
	}
	if _, off := a.DepartureAt.Zone(); off != 3*3600 || a.LocalDeparture().Hour() != 13 {
		t.Fatalf("origin offset kept: %v", a.DepartureAt)
	}
	if b.FlightNumber != "1752" || b.OriginAirport != "IST" || b.DurationMinutes != 400 || b.Transfers != 1 {
		t.Fatalf("numeric flight number, airport fallback, duration fallback: %+v", b)
	}
}

func TestFaresErrors(t *testing.T) {
	status := http.StatusUnauthorized
	body := `{}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	q := domain.FareQuery{Origin: "IST", Destination: "DXB", Month: "2026-10", Currency: "USD"}

	if _, err := NewFares(Config{APIURL: srv.URL}).Fares(context.Background(), q); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("no token: %v", err)
	}
	if _, err := NewFares(Config{APIURL: srv.URL, Token: "bad"}).Fares(context.Background(), q); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("rejected token: %v", err)
	}
	status, body = http.StatusOK, `{"success":false,"error":"wrong origin"}`
	if _, err := NewFares(Config{APIURL: srv.URL, Token: "t"}).Fares(context.Background(), q); err == nil || !strings.Contains(err.Error(), "wrong origin") {
		t.Fatalf("unsuccessful payload: %v", err)
	}
	status = http.StatusBadGateway
	if _, err := NewFares(Config{APIURL: srv.URL, Token: "t"}).Fares(context.Background(), q); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("upstream status: %v", err)
	}
}

func TestPlaces(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		if r.Header.Get("X-Access-Token") != "" {
			t.Error("autocomplete must not receive the token")
		}
		_, _ = w.Write([]byte(`[
			{"type":"city","code":"IST","name":"Istanbul","country_code":"TR","country_name":"Turkiye"},
			{"type":"airport","code":"SAW","name":"Sabiha Gokcen","city_code":"IST","city_name":"Istanbul","country_code":"TR","country_name":"Turkiye"},
			{"type":"country","code":"TR","name":"Turkiye"}
		]`))
	}))
	defer srv.Close()

	places, err := NewPlaces(Config{AutocompleteURL: srv.URL, Token: "secret"}).Places(context.Background(), "ist", "ar")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotQuery, "term=ist") || !strings.Contains(gotQuery, "locale=ar") || strings.Count(gotQuery, "types") != 2 {
		t.Fatalf("query: %s", gotQuery)
	}
	if len(places) != 2 || places[0].CityCode != "IST" || places[1].CityName != "Istanbul" || places[1].Type != "airport" {
		t.Fatalf("places: %+v", places)
	}
}

func TestAirlines(t *testing.T) {
	var hits atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`[{"code":"TK","name":"Turkish Airlines"},{"code":"","name":"x"}]`))
	}))
	defer srv.Close()

	a := NewAirlines(Config{APIURL: srv.URL}, nil)
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	ctx := context.Background()

	if a.AirlineName(ctx, "TK") != "" || a.AirlineName(ctx, "TK") != "" || hits.Load() != 1 {
		t.Fatalf("a failed load is not retried at once (hits=%d)", hits.Load())
	}
	fail.Store(false)
	now = now.Add(airlinesRetry + time.Second)
	if a.AirlineName(ctx, "tk") != "Turkish Airlines" || a.AirlineName(ctx, "ZZ") != "" || hits.Load() != 2 {
		t.Fatalf("retried after the pause and cached (hits=%d)", hits.Load())
	}
	now = now.Add(airlinesTTL + time.Second)
	_ = a.AirlineName(ctx, "TK")
	if hits.Load() != 3 {
		t.Fatalf("refreshed after a day (hits=%d)", hits.Load())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	now = now.Add(airlinesTTL + time.Second)
	if a.AirlineName(cancelled, "TK") != "Turkish Airlines" {
		t.Fatal("a cancelled search still refreshes the shared catalog")
	}
}
