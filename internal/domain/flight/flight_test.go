package flight

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

var today = time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)

func validQuery() Query {
	return Query{Origin: " ist ", Destination: "dxb", Departure: time.Date(2026, 10, 10, 14, 5, 0, 0, time.UTC),
		Passengers: Passengers{Adults: 1}}
}

func TestNormalize(t *testing.T) {
	q := validQuery()
	if err := q.Normalize(today); err != nil {
		t.Fatal(err)
	}
	if q.Origin != "IST" || q.Destination != "DXB" || q.Currency != "USD" {
		t.Fatalf("normalized: %+v", q)
	}

	cases := []struct {
		field string
		edit  func(*Query)
	}{
		{"origin", func(q *Query) { q.Origin = "IS" }},
		{"destination", func(q *Query) { q.Destination = "IST" }},
		{"departure", func(q *Query) { q.Departure = time.Time{} }},
		{"departure", func(q *Query) { q.Departure = today.Add(-time.Minute) }},
		{"departure", func(q *Query) { q.Departure = today.Add(MaxAdvance + time.Hour) }},
		{"adults", func(q *Query) { q.Passengers.Adults = 0 }},
		{"passengers", func(q *Query) { q.Passengers.Children = -1 }},
		{"infants", func(q *Query) { q.Passengers.Infants = 2 }},
		{"passengers", func(q *Query) { q.Passengers = Passengers{Adults: 5, Children: 5} }},
		{"currency", func(q *Query) { q.Currency = "xyz" }},
	}
	for _, c := range cases {
		q := validQuery()
		c.edit(&q)
		err := q.Normalize(today)
		var app *shared.AppError
		if !errors.Is(err, shared.ErrValidation) || !errors.As(err, &app) || app.Details[c.field] == nil {
			t.Errorf("%s: %v", c.field, err)
		}
	}
}

func TestMonths(t *testing.T) {
	q := Query{Departure: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)}
	if got := q.Months(); !reflect.DeepEqual(got, []string{"2026-10", "2026-11"}) {
		t.Fatalf("spans months: %v", got)
	}
	q.Departure = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	if got := q.Months(); !reflect.DeepEqual(got, []string{"2026-10"}) {
		t.Fatalf("single month: %v", got)
	}
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestRank(t *testing.T) {
	wanted := time.Date(2026, 10, 10, 14, 0, 0, 0, time.UTC)
	fares := []Fare{
		{Airline: "A", DepartureAt: at("2026-10-10T16:00:00+03:00"), Price: 300},
		{Airline: "B", DepartureAt: at("2026-10-10T12:00:00+03:00"), Price: 100},
		{Airline: "B", DepartureAt: at("2026-10-10T12:00:00+03:00"), Price: 100},
		{Airline: "C", DepartureAt: at("2026-10-10T13:30:00+03:00"), Price: 500},
		{Airline: "D", DepartureAt: at("2026-10-20T14:00:00+03:00"), Price: 1},
		{Airline: "E", DepartureAt: at("2026-10-10T14:00:00+03:00"), Price: 0},
	}
	got := Rank(fares, wanted, 0)
	if len(got) != 3 {
		t.Fatalf("dedup, window and free fares dropped: %+v", got)
	}
	if got[0].Airline != "C" || got[0].GapMinutes != -30 || !got[0].Closest {
		t.Fatalf("closest first, on the local clock: %+v", got[0])
	}
	if got[1].Airline != "B" || !got[1].Cheapest || got[2].Airline != "A" {
		t.Fatalf("equal gaps by price, cheapest marked: %+v", got)
	}
	if len(Rank(fares, wanted, 1)) != 1 || len(Rank(nil, wanted, 5)) != 0 {
		t.Fatal("limit / empty")
	}
}

func TestAviasalesLinker(t *testing.T) {
	l := AviasalesLinker{BaseURL: "https://www.aviasales.com/", Marker: "578591"}
	f := Fare{Origin: "IST", Destination: "DXB", DepartureAt: at("2026-11-01T09:00:00+03:00"), Link: "/search/IST1012DXB1?t=x"}
	cases := []struct {
		p    Passengers
		link string
		want string
	}{
		{Passengers{Adults: 2, Children: 1}, f.Link, "https://www.aviasales.com/search/IST1012DXB21?marker=578591&t=x"},
		{Passengers{Adults: 1, Infants: 1}, f.Link, "https://www.aviasales.com/search/IST1012DXB101?marker=578591&t=x"},
		{Passengers{Adults: 1}, "/search/IST1012DXB21512?t=y", "https://www.aviasales.com/search/IST1012DXB11512?marker=578591&t=y"},
		{Passengers{Adults: 3}, "", "https://www.aviasales.com/search/IST0111DXB3?marker=578591"},
	}
	for _, c := range cases {
		f.Link = c.link
		if got := l.BookingURL(f, c.p); got != c.want {
			t.Errorf("%+v %q: %s", c.p, c.link, got)
		}
	}

	q := Query{Origin: "IST", Destination: "DAM", Departure: time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC), Passengers: Passengers{Adults: 2, Infants: 1}}
	if got := l.SearchURL(q); got != "https://www.aviasales.com/search/IST0510DAM201?marker=578591" {
		t.Fatalf("search url: %s", got)
	}
	if (AviasalesLinker{}).SearchURL(q) != "" {
		t.Fatal("no base url, no link")
	}
}
