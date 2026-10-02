// Package flight is the flight finder: it asks the fare provider for the
// months around the wanted departure and ranks what it knows by closeness.
package flight

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	fareTTL       = 10 * time.Minute
	placeTTL      = 6 * time.Hour
	cacheEntries  = 512
	resultLimit   = 30
	placeLimit    = 8
	minTermRunes  = 2
	maxTermRunes  = 40
	upstreamRetry = 30 * time.Second
)

// Locales the place directory is asked in; others fall back to English.
var placeLocales = map[string]bool{"en": true, "ar": true}

type Options struct {
	// Enabled is false when no provider token is configured.
	Enabled bool
	Log     *slog.Logger
}

type Service struct {
	fares    domain.FareSource
	places   domain.PlaceDirectory
	airlines domain.AirlineDirectory
	linker   domain.BookingLinker
	enabled  bool
	log      *slog.Logger
	now      func() time.Time

	fareCache  *ttlCache[[]domain.Fare]
	placeCache *ttlCache[[]domain.Place]
}

func NewService(fares domain.FareSource, places domain.PlaceDirectory, airlines domain.AirlineDirectory,
	linker domain.BookingLinker, opts Options) *Service {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	s := &Service{fares: fares, places: places, airlines: airlines, linker: linker,
		enabled: opts.Enabled, log: log, now: time.Now}
	clock := func() time.Time { return s.now() }
	s.fareCache = newTTLCache[[]domain.Fare](fareTTL, cacheEntries, clock)
	s.placeCache = newTTLCache[[]domain.Place](placeTTL, cacheEntries, clock)
	return s
}

// Offer is a ranked fare ready to show and book.
type Offer struct {
	domain.Ranked
	AirlineName string
	BookingURL  string
}

type Result struct {
	Query  domain.Query
	Offers []Offer
	// SearchURL opens the provider's own search for the whole query.
	SearchURL string
	Window    time.Duration
	FetchedAt time.Time
}

func notConfigured() error {
	return &shared.AppError{Code: "flights_not_configured", Message: "flight search is not configured", Err: shared.ErrUnavailable}
}

func unavailable(err error) error {
	return &shared.AppError{Code: "flights_unavailable", Message: "flight provider unavailable",
		Err: errors.Join(shared.ErrUnavailable, err), RetryAfter: upstreamRetry}
}

// Search returns the fares around q.Departure, closest first.
func (s *Service) Search(ctx context.Context, q domain.Query) (*Result, error) {
	// The traveller's day may already have started east of UTC.
	if err := q.Normalize(s.now().UTC().Add(-24 * time.Hour)); err != nil {
		return nil, err
	}
	if !s.enabled {
		return nil, s.withSearchURL(notConfigured(), q)
	}
	fares, err := s.collect(ctx, q)
	if err != nil {
		return nil, s.withSearchURL(err, q)
	}
	ranked := domain.Rank(fares, q.Departure, q.AnyTime, resultLimit)
	offers := make([]Offer, len(ranked))
	for i, r := range ranked {
		offers[i] = Offer{Ranked: r, AirlineName: s.airlines.AirlineName(ctx, r.Airline), BookingURL: s.linker.BookingURL(r.Fare, q.Passengers)}
	}
	return &Result{Query: q, Offers: offers, SearchURL: s.linker.SearchURL(q), Window: domain.SearchWindow, FetchedAt: s.now().UTC()}, nil
}

// withSearchURL lets the traveller continue on the provider's own search when
// no fares can be shown here.
func (s *Service) withSearchURL(err error, q domain.Query) error {
	var app *shared.AppError
	if !errors.As(err, &app) {
		return err
	}
	if link := s.linker.SearchURL(q); link != "" {
		app.Details = map[string]any{"search_url": link}
	}
	return app
}

// collect queries every month of the window (non-stop fares as a second
// pass widen the choice); it fails only when no request succeeded.
func (s *Service) collect(ctx context.Context, q domain.Query) ([]domain.Fare, error) {
	var queries []domain.FareQuery
	for _, month := range q.Months() {
		base := domain.FareQuery{Origin: q.Origin, Destination: q.Destination, Month: month, Currency: q.Currency, DirectOnly: true}
		queries = append(queries, base)
		if !q.DirectOnly {
			base.DirectOnly = false
			queries = append(queries, base)
		}
	}
	type outcome struct {
		fares []domain.Fare
		err   error
	}
	results := make([]outcome, len(queries))
	var wg sync.WaitGroup
	for i, fq := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := s.faresFor(ctx, fq)
			results[i] = outcome{f, err}
		}()
	}
	wg.Wait()

	var all []domain.Fare
	var errs []error
	for _, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		all = append(all, r.fares...)
	}
	if len(errs) == len(queries) {
		if errors.Is(errs[0], domain.ErrProviderRejected) {
			s.log.Error("flight provider rejected the token")
			return nil, notConfigured()
		}
		s.log.Warn("flight provider unavailable", "err", errs[0])
		return nil, unavailable(errs[0])
	}
	if len(errs) > 0 {
		s.log.Warn("flight search partially failed", "failed", len(errs), "of", len(queries), "err", errs[0])
	}
	return all, nil
}

func (s *Service) faresFor(ctx context.Context, fq domain.FareQuery) ([]domain.Fare, error) {
	key := fmt.Sprintf("%s|%s|%s|%s|%t", fq.Origin, fq.Destination, fq.Month, fq.Currency, fq.DirectOnly)
	if f, ok := s.fareCache.get(key); ok {
		return f, nil
	}
	f, err := s.fares.Fares(ctx, fq)
	if err != nil {
		return nil, err
	}
	s.fareCache.put(key, f)
	return f, nil
}

// Places suggests cities and airports; short or overlong terms yield nothing.
func (s *Service) Places(ctx context.Context, term, locale string) ([]domain.Place, error) {
	term = strings.TrimSpace(term)
	if n := utf8.RuneCountInString(term); n < minTermRunes || n > maxTermRunes {
		return []domain.Place{}, nil
	}
	if !placeLocales[locale] {
		locale = "en"
	}
	key := locale + "|" + strings.ToLower(term)
	if p, ok := s.placeCache.get(key); ok {
		return p, nil
	}
	places, err := s.places.Places(ctx, term, locale)
	if err != nil {
		s.log.Warn("flight place lookup unavailable", "err", err)
		return nil, unavailable(err)
	}
	if len(places) > placeLimit {
		places = places[:placeLimit]
	}
	s.placeCache.put(key, places)
	return places, nil
}
