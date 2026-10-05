package tourpackage

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Package kinds and their categories (Umrah tiers / Hajj programmes).
const (
	KindUmrah = "umrah"
	KindHajj  = "hajj"
)

var categoriesByKind = map[string][]string{
	KindUmrah: {"economy", "standard", "luxury", "boutique", "ramadan_first15", "ramadan_last15", "ramadan_full", "semester"},
	KindHajj:  {"short", "long", "special_mujamala", "special_commercial"},
}

var (
	transportModes = []string{"flight_scheduled", "flight_charter", "road"}
	boardTypes     = []string{"bb", "hb", "fb", "tabldot", "buffet"}
	hotelAccess    = []string{"walking", "shuttle"}
	flightRoutings = []string{"direct", "connecting"}
	intercityModes = []string{"haramain_train", "bus"}
	// Visa types mirror the Nusuk Masar enums so bookings can be pushed without mapping.
	visaTypes    = []string{"UMRAH_VISA", "TOURIST_VISA", "PERSONAL_VISIT", "HAJJ_VISA", "BUSINESS_VISIT"}
	kitItems     = []string{"ihram", "luggage", "prayer_book", "zamzam"}
	ziyaratMecca = []string{"thawr", "arafat", "muzdalifah", "mina", "hira", "jannat_al_mualla"}
	ziyaratMed   = []string{"uhud", "qiblatayn", "seven_mosques", "quba", "baqi"}
	itinCities   = []string{"", "makkah", "madinah", "jeddah", "transit"}
)

// Currencies a package may be priced in; SAR is the operational default inside the Kingdom.
var packageCurrencies = []string{"SAR", "USD", "EUR", "TRY", "SYP", "AED", "GBP"}

const DefaultPackageCurrency = "SAR"

var routePattern = regexp.MustCompile(`^[A-Z]{3}(-[A-Z]{3}){1,3}$`)

// Spec is the full product sheet of a package: stay, logistics, services, programme,
// participation rules and the internal cost build-up. Stored as one JSON document.
type Spec struct {
	Nights       Nights         `json:"nights"`
	Makkah       Hotel          `json:"makkah"`
	Madinah      Hotel          `json:"madinah"`
	Flights      Flights        `json:"flights"`
	Transfers    Transfers      `json:"transfers"`
	Guidance     Guidance       `json:"guidance"`
	Visa         Visa           `json:"visa"`
	Kit          []string       `json:"kit"`
	Ziyarat      Ziyarat        `json:"ziyarat"`
	Itinerary    []ItineraryDay `json:"itinerary"`
	Requirements Requirements   `json:"requirements"`
	Costs        Costs          `json:"costs"`
	Included     []string       `json:"included"`
	Excluded     []string       `json:"excluded"`
}

type Nights struct {
	Makkah  int `json:"makkah"`
	Madinah int `json:"madinah"`
}

// Hotel is one city's stay. Distance is to the Haram (Makkah) or Masjid an-Nabawi (Madinah).
type Hotel struct {
	Name           string `json:"name"`
	Stars          int    `json:"stars"`
	DistanceM      int    `json:"distance_m"`
	Access         string `json:"access"`
	ShuttleMinutes int    `json:"shuttle_minutes"`
	Zone           string `json:"zone"`
	Board          string `json:"board"`
	CheckIn        string `json:"check_in"`
	CheckOut       string `json:"check_out"`
}

type FlightLeg struct {
	Route    string `json:"route"`
	FlightNo string `json:"flight_no"`
	Date     string `json:"date"`
}

type Flights struct {
	Airline    string    `json:"airline"`
	Routing    string    `json:"routing"`
	Outbound   FlightLeg `json:"outbound"`
	Inbound    FlightLeg `json:"inbound"`
	PNR        string    `json:"pnr"`
	BlockSeats int       `json:"block_seats"`
}

type Transfers struct {
	Intercity      string `json:"intercity"`
	BusClass       string `json:"bus_class"`
	AirportMeet    bool   `json:"airport_meet"`
	HotelTransfers bool   `json:"hotel_transfers"`
}

type Guidance struct {
	LeaderName  string `json:"leader_name"`
	FemaleGuide bool   `json:"female_guide"`
}

type Visa struct {
	Type            string `json:"type"`
	HealthInsurance bool   `json:"health_insurance"`
}

type Ziyarat struct {
	Makkah  []string `json:"makkah"`
	Madinah []string `json:"madinah"`
}

type ItineraryDay struct {
	Day     int    `json:"day"`
	City    string `json:"city"`
	Title   string `json:"title"`
	Details string `json:"details"`
}

// Requirements are checked against every traveller at booking time.
type Requirements struct {
	PassportMonths int  `json:"passport_months"`
	Meningitis     bool `json:"meningitis"`
	BiometricPhoto bool `json:"biometric_photo"`
	Mahram         bool `json:"mahram"`
	// Women younger than this travel with a mahram (0 = rule applies to all ages).
	MahramMaxAge int `json:"mahram_max_age"`
}

// Costs is the per-person net cost build-up in minor units of Currency (B2B / admin only).
type Costs struct {
	Currency  string `json:"currency"`
	Flight    int64  `json:"flight"`
	Hotel     int64  `json:"hotel"`
	Visa      int64  `json:"visa"`
	Transfer  int64  `json:"transfer"`
	Guidance  int64  `json:"guidance"`
	Gifts     int64  `json:"gifts"`
	MarkupPct int    `json:"markup_pct"`
}

// Total is the per-person net cost.
func (c Costs) Total() int64 {
	return c.Flight + c.Hotel + c.Visa + c.Transfer + c.Guidance + c.Gifts
}

// SuggestedPrice is the net cost plus markup, rounded to whole major units.
func (c Costs) SuggestedPrice() int64 {
	raw := c.Total() * int64(100+c.MarkupPct) / 100
	return (raw + 50) / 100 * 100
}

// DefaultRequirements are the Saudi entry rules most programmes start from.
func DefaultRequirements() Requirements {
	return Requirements{PassportMonths: 6, Meningitis: true, BiometricPhoto: true, Mahram: true, MahramMaxAge: 45}
}

// PassportValidFor reports whether a passport expiring on expiry satisfies the rule for travel on depart.
func (r Requirements) PassportValidFor(expiry, depart time.Time) bool {
	if r.PassportMonths <= 0 {
		return !expiry.Before(depart)
	}
	return !expiry.Before(addMonthsClamped(depart, r.PassportMonths))
}

func addMonthsClamped(t time.Time, months int) time.Time {
	y, m, d := t.Date()
	first := time.Date(y, m+time.Month(months), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	if d > last {
		d = last
	}
	return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, time.UTC)
}

// Header is the identity and sales frame of a package.
type Header struct {
	Kind          string
	Category      string
	DurationDays  int
	TransportMode string
	CapacityTotal int
	BaseCurrency  string
}

// ValidCategory reports whether category belongs to kind.
func ValidCategory(kind, category string) bool {
	return slices.Contains(categoriesByKind[kind], category)
}

// NormalizeHeader trims and validates the header in place.
func NormalizeHeader(h *Header) error {
	h.Kind = strings.ToLower(strings.TrimSpace(h.Kind))
	h.Category = strings.ToLower(strings.TrimSpace(h.Category))
	h.TransportMode = strings.ToLower(strings.TrimSpace(h.TransportMode))
	h.BaseCurrency = strings.ToUpper(strings.TrimSpace(h.BaseCurrency))
	if h.Kind == "" {
		h.Kind = KindUmrah
	}
	if h.BaseCurrency == "" {
		h.BaseCurrency = DefaultPackageCurrency
	}
	if h.TransportMode == "" {
		h.TransportMode = "flight_scheduled"
	}
	if _, ok := categoriesByKind[h.Kind]; !ok {
		return shared.NewValidation("kind must be umrah or hajj")
	}
	if h.Category == "" {
		h.Category = categoriesByKind[h.Kind][1%len(categoriesByKind[h.Kind])]
	}
	if !ValidCategory(h.Kind, h.Category) {
		return shared.NewValidation(fmt.Sprintf("category %q is not valid for %s", h.Category, h.Kind))
	}
	if !slices.Contains(transportModes, h.TransportMode) {
		return shared.NewValidation("transport_mode must be flight_scheduled, flight_charter or road")
	}
	if !slices.Contains(packageCurrencies, h.BaseCurrency) {
		return shared.NewValidation("unsupported base_currency " + h.BaseCurrency)
	}
	if h.DurationDays < 0 || h.DurationDays > 60 {
		return shared.NewValidation("duration_days must be between 1 and 60")
	}
	if h.CapacityTotal < 0 || h.CapacityTotal > 10000 {
		return shared.NewValidation("capacity_total must be between 0 and 10000")
	}
	return nil
}

// NormalizeSpec trims, canonicalises and validates the spec against its header.
func NormalizeSpec(s *Spec, h Header) error {
	if s.Nights.Makkah < 0 || s.Nights.Madinah < 0 {
		return shared.NewValidation("nights cannot be negative")
	}
	if h.DurationDays > 0 && s.Nights.Makkah+s.Nights.Madinah > h.DurationDays {
		return shared.NewValidation("makkah + madinah nights cannot exceed duration_days")
	}
	if err := normalizeHotel(&s.Makkah, "makkah", s.Nights.Makkah); err != nil {
		return err
	}
	if err := normalizeHotel(&s.Madinah, "madinah", s.Nights.Madinah); err != nil {
		return err
	}
	if err := normalizeFlights(&s.Flights); err != nil {
		return err
	}
	if err := normalizeTransfers(&s.Transfers); err != nil {
		return err
	}
	s.Guidance.LeaderName = clip(s.Guidance.LeaderName, 120)
	s.Visa.Type = strings.ToUpper(strings.TrimSpace(s.Visa.Type))
	if s.Visa.Type != "" && !slices.Contains(visaTypes, s.Visa.Type) {
		return shared.NewValidation("visa.type must be one of " + strings.Join(visaTypes, ", "))
	}
	var err error
	if s.Kit, err = normalizeSet(s.Kit, kitItems, "kit"); err != nil {
		return err
	}
	if s.Ziyarat.Makkah, err = normalizeSet(s.Ziyarat.Makkah, ziyaratMecca, "ziyarat.makkah"); err != nil {
		return err
	}
	if s.Ziyarat.Madinah, err = normalizeSet(s.Ziyarat.Madinah, ziyaratMed, "ziyarat.madinah"); err != nil {
		return err
	}
	if err := normalizeItinerary(s, h.DurationDays); err != nil {
		return err
	}
	if err := normalizeRequirements(&s.Requirements); err != nil {
		return err
	}
	if err := normalizeCosts(&s.Costs, h.BaseCurrency); err != nil {
		return err
	}
	s.Included = normalizeLines(s.Included)
	s.Excluded = normalizeLines(s.Excluded)
	return nil
}

func normalizeHotel(hotel *Hotel, city string, nights int) error {
	hotel.Name = clip(hotel.Name, 160)
	hotel.Zone = clip(hotel.Zone, 80)
	hotel.Access = strings.ToLower(strings.TrimSpace(hotel.Access))
	hotel.Board = strings.ToLower(strings.TrimSpace(hotel.Board))
	hotel.CheckIn = strings.TrimSpace(hotel.CheckIn)
	hotel.CheckOut = strings.TrimSpace(hotel.CheckOut)
	if hotel.Stars < 0 || hotel.Stars > 5 {
		return shared.NewValidation(city + ".stars must be between 1 and 5")
	}
	if hotel.DistanceM < 0 || hotel.DistanceM > 50000 {
		return shared.NewValidation(city + ".distance_m must be between 0 and 50000")
	}
	if hotel.Access != "" && !slices.Contains(hotelAccess, hotel.Access) {
		return shared.NewValidation(city + ".access must be walking or shuttle")
	}
	if hotel.Access != "shuttle" {
		hotel.ShuttleMinutes = 0
	}
	if hotel.ShuttleMinutes < 0 || hotel.ShuttleMinutes > 180 {
		return shared.NewValidation(city + ".shuttle_minutes must be between 0 and 180")
	}
	if hotel.Board != "" && !slices.Contains(boardTypes, hotel.Board) {
		return shared.NewValidation(city + ".board must be bb, hb, fb, tabldot or buffet")
	}
	in, err := optionalDay(hotel.CheckIn, city+".check_in")
	if err != nil {
		return err
	}
	out, err := optionalDay(hotel.CheckOut, city+".check_out")
	if err != nil {
		return err
	}
	if !in.IsZero() && !out.IsZero() {
		if !out.After(in) {
			return shared.NewValidation(city + ".check_out must be after check_in")
		}
		if stay := int(out.Sub(in).Hours() / 24); nights > 0 && stay != nights {
			return shared.NewValidation(fmt.Sprintf("%s stay is %d nights but %d nights are planned", city, stay, nights))
		}
	}
	return nil
}

func normalizeFlights(f *Flights) error {
	f.Airline = clip(f.Airline, 80)
	f.Routing = strings.ToLower(strings.TrimSpace(f.Routing))
	f.PNR = strings.ToUpper(clip(f.PNR, 24))
	if f.Routing != "" && !slices.Contains(flightRoutings, f.Routing) {
		return shared.NewValidation("flights.routing must be direct or connecting")
	}
	if f.BlockSeats < 0 || f.BlockSeats > 2000 {
		return shared.NewValidation("flights.block_seats must be between 0 and 2000")
	}
	for name, leg := range map[string]*FlightLeg{"outbound": &f.Outbound, "inbound": &f.Inbound} {
		leg.Route = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(leg.Route), " ", ""))
		leg.FlightNo = strings.ToUpper(clip(leg.FlightNo, 16))
		if leg.Route != "" && !routePattern.MatchString(leg.Route) {
			return shared.NewValidation("flights." + name + ".route must look like IST-JED")
		}
		if _, err := optionalDay(leg.Date, "flights."+name+".date"); err != nil {
			return err
		}
	}
	if f.Outbound.Date != "" && f.Inbound.Date != "" && f.Inbound.Date < f.Outbound.Date {
		return shared.NewValidation("flights.inbound.date must be on or after the outbound date")
	}
	return nil
}

func normalizeTransfers(t *Transfers) error {
	t.Intercity = strings.ToLower(strings.TrimSpace(t.Intercity))
	t.BusClass = clip(t.BusClass, 120)
	if t.Intercity != "" && !slices.Contains(intercityModes, t.Intercity) {
		return shared.NewValidation("transfers.intercity must be haramain_train or bus")
	}
	return nil
}

func normalizeItinerary(s *Spec, days int) error {
	if len(s.Itinerary) > 60 {
		return shared.NewValidation("itinerary cannot have more than 60 days")
	}
	seen := map[int]struct{}{}
	out := s.Itinerary[:0]
	for _, d := range s.Itinerary {
		d.Title = clip(d.Title, 160)
		d.Details = clip(d.Details, 2000)
		d.City = strings.ToLower(strings.TrimSpace(d.City))
		if d.Title == "" && d.Details == "" {
			continue
		}
		if d.Day < 1 || (days > 0 && d.Day > days) {
			return shared.NewValidation(fmt.Sprintf("itinerary day %d is outside the %d-day programme", d.Day, days))
		}
		if _, dup := seen[d.Day]; dup {
			return shared.NewValidation(fmt.Sprintf("itinerary day %d appears twice", d.Day))
		}
		if !slices.Contains(itinCities, d.City) {
			return shared.NewValidation("itinerary city must be makkah, madinah, jeddah or transit")
		}
		if d.Title == "" {
			return shared.NewValidation(fmt.Sprintf("itinerary day %d needs a title", d.Day))
		}
		seen[d.Day] = struct{}{}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	s.Itinerary = out
	return nil
}

func normalizeRequirements(r *Requirements) error {
	if r.PassportMonths < 0 || r.PassportMonths > 24 {
		return shared.NewValidation("requirements.passport_months must be between 0 and 24")
	}
	if r.MahramMaxAge < 0 || r.MahramMaxAge > 80 {
		return shared.NewValidation("requirements.mahram_max_age must be between 0 and 80")
	}
	if !r.Mahram {
		r.MahramMaxAge = 0
	}
	return nil
}

func normalizeCosts(c *Costs, base string) error {
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	if c.Currency == "" {
		c.Currency = base
	}
	if !slices.Contains(packageCurrencies, c.Currency) {
		return shared.NewValidation("unsupported costs.currency " + c.Currency)
	}
	for _, v := range []int64{c.Flight, c.Hotel, c.Visa, c.Transfer, c.Guidance, c.Gifts} {
		if v < 0 {
			return shared.NewValidation("cost lines cannot be negative")
		}
	}
	if c.MarkupPct < 0 || c.MarkupPct > 300 {
		return shared.NewValidation("costs.markup_pct must be between 0 and 300")
	}
	return nil
}

func normalizeSet(in, allowed []string, field string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || slices.Contains(out, v) {
			continue
		}
		if !slices.Contains(allowed, v) {
			return nil, shared.NewValidation(fmt.Sprintf("%s: unknown value %q", field, v))
		}
		out = append(out, v)
	}
	// Canonical order keeps diffs and printed programmes stable.
	sort.SliceStable(out, func(i, j int) bool { return slices.Index(allowed, out[i]) < slices.Index(allowed, out[j]) })
	return out, nil
}

func normalizeLines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = clip(v, 200); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
		if len(out) == 40 {
			break
		}
	}
	return out
}

func optionalDay(v, field string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return time.Time{}, shared.NewValidation(field + " must be YYYY-MM-DD")
	}
	return t, nil
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}
