// Package flight models the flight finder: a traveller's request, the fares
// a provider knows for it, and how they are ranked around the wanted time.
package flight

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	MaxTravellers = 9
	// SearchWindow is how far either side of the wanted departure fares are kept.
	SearchWindow = 3 * 24 * time.Hour
	// MaxAdvance bounds how far ahead a search may look (provider data ends there).
	MaxAdvance = 365 * 24 * time.Hour
	// WallClock is the layout of a local departure ("2026-10-10T14:05").
	WallClock = "2006-01-02T15:04"
)

var iata = regexp.MustCompile(`^[A-Z]{3}$`)

// ErrProviderRejected means the fare provider refused the credentials (or none are set).
var ErrProviderRejected = errors.New("flight provider rejected the credentials")

// Currencies are the price currencies offered in the finder.
var Currencies = []string{"USD", "EUR", "SAR", "AED", "TRY", "GBP", "QAR", "KWD", "JOD", "EGP"}

// Passengers is who travels; infants sit on an adult's lap.
type Passengers struct {
	Adults   int
	Children int
	Infants  int
}

func (p Passengers) Total() int { return p.Adults + p.Children + p.Infants }

// Query is a one-way search. Departure is the wanted local wall-clock time at
// the origin, stored as UTC fields (no zone): fares are compared on the clock.
type Query struct {
	Origin      string
	Destination string
	Departure   time.Time
	Passengers  Passengers
	Currency    string
	DirectOnly  bool
}

// Normalize upper-cases codes and validates the query against today (wall clock).
func (q *Query) Normalize(today time.Time) error {
	q.Origin = strings.ToUpper(strings.TrimSpace(q.Origin))
	q.Destination = strings.ToUpper(strings.TrimSpace(q.Destination))
	q.Currency = strings.ToUpper(strings.TrimSpace(q.Currency))
	if q.Currency == "" {
		q.Currency = "USD"
	}
	fields := map[string]any{}
	if !iata.MatchString(q.Origin) {
		fields["origin"] = "IATA code required"
	}
	if !iata.MatchString(q.Destination) {
		fields["destination"] = "IATA code required"
	}
	if fields["origin"] == nil && q.Origin == q.Destination {
		fields["destination"] = "must differ from origin"
	}
	day := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case q.Departure.IsZero():
		fields["departure"] = "required"
	case q.Departure.Before(day):
		fields["departure"] = "in the past"
	case q.Departure.After(day.Add(MaxAdvance)):
		fields["departure"] = "at most one year ahead"
	}
	p := q.Passengers
	switch {
	case p.Adults < 1:
		fields["adults"] = "at least one adult"
	case p.Children < 0 || p.Infants < 0:
		fields["passengers"] = "invalid count"
	case p.Infants > p.Adults:
		fields["infants"] = "at most one infant per adult"
	case p.Total() > MaxTravellers:
		fields["passengers"] = "at most 9 travellers"
	}
	if !validCurrency(q.Currency) {
		fields["currency"] = "unsupported"
	}
	if len(fields) > 0 {
		err := shared.NewValidation("invalid flight search")
		err.Details = fields
		return err
	}
	return nil
}

func validCurrency(c string) bool {
	for _, x := range Currencies {
		if x == c {
			return true
		}
	}
	return false
}

// Months are the "YYYY-MM" periods the search window touches, in order.
func (q Query) Months() []string {
	from, to := q.Departure.Add(-SearchWindow), q.Departure.Add(SearchWindow)
	var out []string
	for m := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC); !m.After(to); m = m.AddDate(0, 1, 0) {
		out = append(out, m.Format("2006-01"))
	}
	return out
}

// Fare is one priced ticket a provider has seen (prices are cached, not live).
type Fare struct {
	Origin             string
	Destination        string
	OriginAirport      string
	DestinationAirport string
	Airline            string
	FlightNumber       string
	// DepartureAt carries the origin's UTC offset.
	DepartureAt     time.Time
	DurationMinutes int
	Transfers       int
	Price           float64
	Currency        string
	// Link is the provider's relative search path for this exact ticket.
	Link string
}

// Key identifies the same ticket returned by overlapping provider queries.
func (f Fare) Key() string {
	return f.Airline + f.FlightNumber + f.OriginAirport + f.DestinationAirport + f.DepartureAt.Format(time.RFC3339)
}

// LocalDeparture is the departure on the origin's wall clock, as stored in Query.
func (f Fare) LocalDeparture() time.Time {
	t := f.DepartureAt
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC)
}

// FareQuery is one provider request: a route and a month ("YYYY-MM").
type FareQuery struct {
	Origin      string
	Destination string
	Month       string
	DirectOnly  bool
	Currency    string
}

// Place is a city or airport a traveller can pick.
type Place struct {
	Code        string
	Type        string // city | airport
	Name        string
	CityCode    string
	CityName    string
	CountryCode string
	CountryName string
}

// FareSource returns cached fares for a route and month.
type FareSource interface {
	Fares(ctx context.Context, q FareQuery) ([]Fare, error)
}

// PlaceDirectory suggests cities and airports for a typed term.
type PlaceDirectory interface {
	Places(ctx context.Context, term, locale string) ([]Place, error)
}

// AirlineDirectory names airlines by IATA code ("" when unknown).
type AirlineDirectory interface {
	AirlineName(ctx context.Context, code string) string
}

// BookingLinker turns a fare (or a whole query) into the URL where it can be booked.
type BookingLinker interface {
	BookingURL(f Fare, p Passengers) string
	SearchURL(q Query) string
}
