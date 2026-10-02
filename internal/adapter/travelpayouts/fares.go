package travelpayouts

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
)

// fareLimit caps one month's rows; one-way results hold one fare per day.
const fareLimit = 100

// Fares reads `GET /aviasales/v3/prices_for_dates` (cached prices of the
// last days' searches, one cheapest fare per departure day).
type Fares struct {
	cfg Config
	t   transport
}

func NewFares(cfg Config) *Fares {
	cfg = cfg.withDefaults()
	return &Fares{cfg: cfg, t: newTransport(cfg)}
}

type pricesResponse struct {
	Success  bool       `json:"success"`
	Error    string     `json:"error"`
	Currency string     `json:"currency"`
	Data     []priceRow `json:"data"`
}

type priceRow struct {
	Origin             string  `json:"origin"`
	Destination        string  `json:"destination"`
	OriginAirport      string  `json:"origin_airport"`
	DestinationAirport string  `json:"destination_airport"`
	Price              float64 `json:"price"`
	Airline            string  `json:"airline"`
	FlightNumber       any     `json:"flight_number"`
	DepartureAt        string  `json:"departure_at"`
	Transfers          int     `json:"transfers"`
	Duration           int     `json:"duration"`
	DurationTo         int     `json:"duration_to"`
	Link               string  `json:"link"`
}

func (f *Fares) Fares(ctx context.Context, q domain.FareQuery) ([]domain.Fare, error) {
	if f.cfg.Token == "" {
		return nil, ErrUnauthorized
	}
	v := url.Values{}
	v.Set("origin", q.Origin)
	v.Set("destination", q.Destination)
	v.Set("departure_at", q.Month)
	v.Set("one_way", "true")
	v.Set("direct", strconv.FormatBool(q.DirectOnly))
	v.Set("sorting", "price")
	v.Set("currency", strings.ToLower(q.Currency))
	v.Set("limit", strconv.Itoa(fareLimit))
	if f.cfg.Market != "" {
		v.Set("market", f.cfg.Market)
	}
	var res pricesResponse
	if err := f.t.getJSON(ctx, f.cfg.APIURL+"/aviasales/v3/prices_for_dates?"+v.Encode(), true, &res); err != nil {
		return nil, err
	}
	if !res.Success {
		msg := res.Error
		if msg == "" {
			msg = "unsuccessful response"
		}
		return nil, errors.New("travelpayouts: " + msg)
	}
	currency := strings.ToUpper(res.Currency)
	if currency == "" {
		currency = strings.ToUpper(q.Currency)
	}
	out := make([]domain.Fare, 0, len(res.Data))
	for _, r := range res.Data {
		dep, err := time.Parse(time.RFC3339, r.DepartureAt)
		if err != nil {
			continue
		}
		duration := r.DurationTo
		if duration == 0 {
			duration = r.Duration
		}
		out = append(out, domain.Fare{
			Origin: r.Origin, Destination: r.Destination,
			OriginAirport: orDefault(r.OriginAirport, r.Origin), DestinationAirport: orDefault(r.DestinationAirport, r.Destination),
			Airline: strings.ToUpper(r.Airline), FlightNumber: flightNumber(r.FlightNumber),
			DepartureAt: dep, DurationMinutes: duration, Transfers: r.Transfers,
			Price: r.Price, Currency: currency, Link: r.Link,
		})
	}
	return out, nil
}

// flightNumber accepts the provider's number or string form.
func flightNumber(v any) string {
	switch n := v.(type) {
	case string:
		return strings.TrimSpace(n)
	case float64:
		return strconv.FormatInt(int64(n), 10)
	default:
		return ""
	}
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
