// Package flight exposes the flight finder: place suggestions and fare search.
package flight

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	appflight "github.com/wodi-crm/wodi-crm-be/internal/app/flight"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Handler struct {
	Svc *appflight.Service
}

// Places is `GET /v1/flights/places?term=&locale=`.
func (h Handler) Places(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	places, err := h.Svc.Places(r.Context(), q.Get("term"), q.Get("locale"))
	if err != nil {
		response.Error(w, err)
		return
	}
	out := make([]map[string]any, len(places))
	for i, p := range places {
		out[i] = map[string]any{
			"code": p.Code, "type": p.Type, "name": p.Name, "city_code": p.CityCode, "city_name": p.CityName,
			"country_code": p.CountryCode, "country_name": p.CountryName,
		}
	}
	response.JSON(w, http.StatusOK, out)
}

// Search is `GET /v1/flights/search?origin=&destination=&date=YYYY-MM-DD&time=HH:MM
// &adults=&children=&infants=&currency=&direct=`; time is the origin's local clock
// and optional (without it the whole day is searched).
func (h Handler) Search(w http.ResponseWriter, r *http.Request) {
	q, err := parseQuery(r)
	if err != nil {
		response.Error(w, err)
		return
	}
	res, err := h.Svc.Search(r.Context(), q)
	if err != nil {
		response.Error(w, err)
		return
	}
	response.JSON(w, http.StatusOK, mapResult(res))
}

func parseQuery(r *http.Request) (domain.Query, error) {
	v := r.URL.Query()
	fields := map[string]any{}
	clock := strings.TrimSpace(v.Get("time"))
	anyTime := clock == ""
	if anyTime {
		clock = "00:00"
	}
	departure, err := time.Parse("2006-01-02 15:04", strings.TrimSpace(v.Get("date"))+" "+clock)
	if err != nil {
		fields["departure"] = "date YYYY-MM-DD and optional time HH:MM required"
	}
	count := func(key string, fallback int) int {
		raw := strings.TrimSpace(v.Get(key))
		if raw == "" {
			return fallback
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			fields[key] = "number required"
		}
		return n
	}
	q := domain.Query{
		Origin: v.Get("origin"), Destination: v.Get("destination"), Departure: departure, AnyTime: anyTime,
		Passengers: domain.Passengers{Adults: count("adults", 1), Children: count("children", 0), Infants: count("infants", 0)},
		Currency:   v.Get("currency"),
		DirectOnly: v.Get("direct") == "true" || v.Get("direct") == "1",
	}
	if len(fields) > 0 {
		e := shared.NewValidation("invalid flight search")
		e.Details = fields
		return q, e
	}
	return q, nil
}

func mapResult(res *appflight.Result) map[string]any {
	offers := make([]map[string]any, len(res.Offers))
	for i, o := range res.Offers {
		offers[i] = map[string]any{
			"origin": o.Origin, "destination": o.Destination,
			"origin_airport": o.OriginAirport, "destination_airport": o.DestinationAirport,
			"airline": o.Airline, "airline_name": o.AirlineName, "flight_number": o.FlightNumber,
			"departure_at":     o.DepartureAt.Format(time.RFC3339),
			"local_departure":  o.LocalDeparture().Format(domain.WallClock),
			"duration_minutes": o.DurationMinutes, "transfers": o.Transfers,
			"price": o.Price, "currency": o.Currency,
			"gap_minutes": o.GapMinutes, "closest": o.Closest, "cheapest": o.Cheapest,
			"booking_url": o.BookingURL,
		}
	}
	q := res.Query
	return map[string]any{
		"query": map[string]any{
			"origin": q.Origin, "destination": q.Destination, "departure": q.Departure.Format(domain.WallClock),
			"adults": q.Passengers.Adults, "children": q.Passengers.Children, "infants": q.Passengers.Infants,
			"currency": q.Currency, "direct": q.DirectOnly, "any_time": q.AnyTime,
		},
		"search_url":   res.SearchURL,
		"window_hours": int(res.Window / time.Hour),
		"fetched_at":   res.FetchedAt.Format(time.RFC3339),
		"offers":       offers,
	}
}
