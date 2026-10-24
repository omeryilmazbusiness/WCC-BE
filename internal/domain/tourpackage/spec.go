package tourpackage

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Package kinds. Other is a custom tour to a free-text destination.
const (
	KindUmrah = "umrah"
	KindHajj  = "hajj"
	KindOther = "other"
)

// PackageTiers are the only categories offered on a new package (all kinds).
var PackageTiers = []string{"economy", "standard", "luxury"}

// Legacy categories stay valid so existing product sheets still load and save.
var legacyCategoriesByKind = map[string][]string{
	KindUmrah: {"boutique", "ramadan_first15", "ramadan_last15", "ramadan_full", "semester"},
	KindHajj:  {"short", "long", "special_mujamala", "special_commercial"},
	KindOther: {},
}

var (
	transportModes = []string{"flight_scheduled", "flight_charter", "road"}
	boardTypes     = []string{"bb", "hb", "fb", "tabldot", "buffet"}
	hotelAccess    = []string{"walking", "shuttle"}
	flightRoutings = []string{"direct", "connecting"}
	intercityModes = []string{"haramain_train", "bus", "private_car"}
	// Visa types mirror the Nusuk Masar enums so bookings can be pushed without mapping.
	visaTypes    = []string{"UMRAH_VISA", "TOURIST_VISA", "PERSONAL_VISIT", "HAJJ_VISA", "BUSINESS_VISIT"}
	kitItems     = []string{"ihram", "luggage", "prayer_book", "zamzam"}
	ziyaratMecca = []string{"thawr", "arafat", "muzdalifah", "mina", "hira", "jannat_al_mualla"}
	ziyaratMed   = []string{"uhud", "qiblatayn", "seven_mosques", "quba", "baqi"}
	itinCities   = []string{"", "makkah", "madinah", "jeddah", "transit"}
)

// IsPilgrimage reports whether kind is an Umrah or Hajj programme.
func IsPilgrimage(kind string) bool {
	return kind == KindUmrah || kind == KindHajj
}

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
	Destination  Destination    `json:"destination"`
	Stays        []Stay         `json:"stays"`
	Bus          Bus            `json:"bus"`
	Services     []Activity     `json:"services"`
	Activities   []Activity     `json:"activities"`
	Itinerary    []ItineraryDay `json:"itinerary"`
	Requirements Requirements   `json:"requirements"`
	Costs        Costs          `json:"costs"`
	Included     []string       `json:"included"`
	Excluded     []string       `json:"excluded"`
}

// Destination is the country + districts of a custom (other) tour.
type Destination struct {
	Country   string   `json:"country"`
	Districts []string `json:"districts"`
}

// Stay is one city hotel night-block on a custom tour.
type Stay struct {
	City   string `json:"city"`
	Nights int    `json:"nights"`
	Hotel  Hotel  `json:"hotel"`
}

// Bus is a road coach booked like a flight block: operator, seats, price, notes.
type Bus struct {
	Company string `json:"company"`
	Seats   int    `json:"seats"`
	Price   int64  `json:"price"`
	Details string `json:"details"`
}

// Activity is a titled free-text item: a custom-tour service or a programme activity.
type Activity struct {
	Title   string `json:"title"`
	Details string `json:"details"`
}

type Nights struct {
	Makkah  int `json:"makkah"`
	Madinah int `json:"madinah"`
}

// Hotel is one city's stay. Distance is to the Haram (Makkah) or Masjid an-Nabawi (Madinah).
type Hotel struct {
	// HotelID links the stay to a contracted hotel; blank for free text.
	HotelID        string `json:"hotel_id,omitempty"`
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

// ValidKind reports whether kind is a supported package type.
func ValidKind(kind string) bool {
	return kind == KindUmrah || kind == KindHajj || kind == KindOther
}

// ValidCategory reports whether category belongs to kind (tier or a legacy value).
func ValidCategory(kind, category string) bool {
	if !ValidKind(kind) {
		return false
	}
	return slices.Contains(PackageTiers, category) || slices.Contains(legacyCategoriesByKind[kind], category)
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
	if !ValidKind(h.Kind) {
		return shared.NewValidation("kind must be umrah, hajj or other")
	}
	if h.Category == "" {
		h.Category = "standard"
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
	if IsPilgrimage(h.Kind) && h.DurationDays > 0 && s.Nights.Makkah+s.Nights.Madinah > h.DurationDays {
		return shared.NewValidation("makkah + madinah nights cannot exceed duration_days")
	}
	if err := normalizeHotel(&s.Makkah, "makkah", s.Nights.Makkah); err != nil {
		return err
	}
	if err := normalizeHotel(&s.Madinah, "madinah", s.Nights.Madinah); err != nil {
		return err
	}
	if err := normalizeDestination(&s.Destination, h.Kind == KindOther); err != nil {
		return err
	}
	if err := normalizeStays(&s.Stays, h); err != nil {
		return err
	}
	if err := normalizeFlights(&s.Flights); err != nil {
		return err
	}
	if err := normalizeBus(&s.Bus); err != nil {
		return err
	}
	if err := normalizeTransfers(&s.Transfers); err != nil {
		return err
	}
	s.Guidance.LeaderName = clip(s.Guidance.LeaderName, 120)
	if h.Kind == KindOther {
		s.Visa = Visa{}
		s.Kit = nil
		s.Ziyarat = Ziyarat{}
		s.Guidance.FemaleGuide = false
		s.Included = nil
	} else {
		s.Services = nil
		s.Activities = nil
	}
	s.Visa.Type = strings.ToUpper(strings.TrimSpace(s.Visa.Type))
	if s.Visa.Type != "" && !slices.Contains(visaTypes, s.Visa.Type) {
		return shared.NewValidation("visa.type must be one of " + strings.Join(visaTypes, ", "))
	}
	var err error
	if s.Kit, err = normalizeFlexibleSet(s.Kit, kitItems, "kit"); err != nil {
		return err
	}
	if s.Ziyarat.Makkah, err = normalizeFlexibleSet(s.Ziyarat.Makkah, ziyaratMecca, "ziyarat.makkah"); err != nil {
		return err
	}
	if s.Ziyarat.Madinah, err = normalizeFlexibleSet(s.Ziyarat.Madinah, ziyaratMed, "ziyarat.madinah"); err != nil {
		return err
	}
	if s.Services, err = normalizeNamedItems(s.Services, "services"); err != nil {
		return err
	}
	if s.Activities, err = normalizeNamedItems(s.Activities, "activities"); err != nil {
		return err
	}
	if err := normalizeItinerary(s, h); err != nil {
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
	hotel.HotelID = strings.TrimSpace(hotel.HotelID)
	if hotel.HotelID != "" {
		if _, err := uuid.Parse(hotel.HotelID); err != nil {
			return shared.NewValidation(city + ".hotel_id must be a UUID")
		}
	}
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
		return shared.NewValidation("transfers.intercity must be haramain_train, bus or private_car")
	}
	return nil
}

func normalizeDestination(d *Destination, required bool) error {
	d.Country = clip(d.Country, 80)
	out := make([]string, 0, len(d.Districts))
	for _, v := range d.Districts {
		if v = clip(v, 80); v == "" || containsFold(out, v) {
			continue
		}
		out = append(out, v)
		if len(out) == 20 {
			break
		}
	}
	d.Districts = out
	if !required {
		return nil
	}
	if len([]rune(d.Country)) < 2 {
		return shared.NewValidation("destination.country is required")
	}
	if len(d.Districts) == 0 {
		return shared.NewValidation("destination.districts must have at least one city")
	}
	return nil
}

func normalizeStays(stays *[]Stay, h Header) error {
	in := *stays
	out := make([]Stay, 0, len(in))
	total := 0
	for i := range in {
		s := in[i]
		s.City = clip(s.City, 80)
		if s.Nights < 0 || s.Nights > 60 {
			return shared.NewValidation("stays.nights must be between 0 and 60")
		}
		if err := normalizeHotel(&s.Hotel, "stays", s.Nights); err != nil {
			return err
		}
		if s.City == "" && s.Hotel.Name == "" && s.Nights == 0 {
			continue
		}
		if s.City == "" {
			return shared.NewValidation("stays.city is required")
		}
		total += s.Nights
		out = append(out, s)
		if len(out) == 20 {
			break
		}
	}
	if h.Kind == KindOther && h.DurationDays > 0 && total > h.DurationDays {
		return shared.NewValidation("stay nights cannot exceed duration_days")
	}
	*stays = out
	return nil
}

func normalizeBus(b *Bus) error {
	b.Company = clip(b.Company, 80)
	b.Details = clip(b.Details, 2000)
	if b.Seats < 0 || b.Seats > 2000 {
		return shared.NewValidation("bus.seats must be between 0 and 2000")
	}
	if b.Price < 0 {
		return shared.NewValidation("bus.price cannot be negative")
	}
	return nil
}

func normalizeNamedItems(in []Activity, field string) ([]Activity, error) {
	out := make([]Activity, 0, len(in))
	for _, a := range in {
		a.Title = clip(a.Title, 160)
		a.Details = clip(a.Details, 2000)
		if a.Title == "" && a.Details == "" {
			continue
		}
		if a.Title == "" {
			return nil, shared.NewValidation(field + " item needs a title")
		}
		out = append(out, a)
		if len(out) == 40 {
			break
		}
	}
	return out, nil
}

func normalizeItinerary(s *Spec, h Header) error {
	days := h.DurationDays
	if len(s.Itinerary) > 60 {
		return shared.NewValidation("itinerary cannot have more than 60 days")
	}
	seen := map[int]struct{}{}
	out := s.Itinerary[:0]
	for _, d := range s.Itinerary {
		d.Title = clip(d.Title, 160)
		d.Details = clip(d.Details, 2000)
		if h.Kind == KindOther {
			d.City = clip(d.City, 80)
		} else {
			d.City = strings.ToLower(strings.TrimSpace(d.City))
		}
		if d.Title == "" && d.Details == "" {
			continue
		}
		if d.Day < 1 || (days > 0 && d.Day > days) {
			return shared.NewValidation(fmt.Sprintf("itinerary day %d is outside the %d-day programme", d.Day, days))
		}
		if _, dup := seen[d.Day]; dup {
			return shared.NewValidation(fmt.Sprintf("itinerary day %d appears twice", d.Day))
		}
		if h.Kind != KindOther && !slices.Contains(itinCities, d.City) {
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
	return normalizeFlexibleSet(in, allowed, field)
}

func normalizeFlexibleSet(in, presets []string, field string) ([]string, error) {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		lower := strings.ToLower(v)
		if slices.Contains(presets, lower) {
			v = lower
		} else {
			v = clip(v, 80)
			if len([]rune(v)) < 2 {
				return nil, shared.NewValidation(fmt.Sprintf("%s: item is too short", field))
			}
		}
		if containsFold(out, v) {
			continue
		}
		out = append(out, v)
		if len(out) == 40 {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ii, jj := slices.Index(presets, strings.ToLower(out[i])), slices.Index(presets, strings.ToLower(out[j]))
		if ii >= 0 && jj >= 0 {
			return ii < jj
		}
		if ii >= 0 {
			return true
		}
		if jj >= 0 {
			return false
		}
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out, nil
}

func containsFold(in []string, v string) bool {
	for _, x := range in {
		if strings.EqualFold(x, v) {
			return true
		}
	}
	return false
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
