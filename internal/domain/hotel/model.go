// Package hotel is the contracting master data of the agency: hotel
// profiles, seasonal net rate matrices, child and cancellation policies,
// allotments and stop sales, plus the quote engine that prices a stay
// from them.
package hotel

import (
	"context"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Room types the rate matrix can be keyed by.
const (
	RoomStandard = "standard"
	RoomSuperior = "superior"
	RoomDeluxe   = "deluxe"
	RoomFamily   = "family"
	RoomSuite    = "suite"
	RoomTriple   = "triple"
	RoomQuad     = "quad"
)

var RoomTypes = []string{RoomStandard, RoomSuperior, RoomDeluxe, RoomFamily, RoomSuite, RoomTriple, RoomQuad}

// Meal plans (board bases).
const (
	MealRO = "ro"
	MealBB = "bb"
	MealHB = "hb"
	MealFB = "fb"
	MealAI = "ai"
)

var MealPlans = []string{MealRO, MealBB, MealHB, MealFB, MealAI}

// Landmarks the distance is measured to.
const (
	LandmarkHaram   = "haram"
	LandmarkNabawi  = "nabawi"
	LandmarkCenter  = "city_center"
	LandmarkAirport = "airport"
)

var Landmarks = []string{LandmarkHaram, LandmarkNabawi, LandmarkCenter, LandmarkAirport}

const (
	MaxNameLen  = 160
	MaxNotesLen = 4000
	MaxDistance = 100_000
)

var (
	countryPattern  = regexp.MustCompile(`^[A-Z]{2}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	phonePattern    = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{5,19}$`)
)

// Contact is the contracting counterpart at the hotel.
type Contact struct {
	SalesName         string `json:"sales_name"`
	SalesPhone        string `json:"sales_phone"`
	SalesEmail        string `json:"sales_email"`
	ReservationsEmail string `json:"reservations_email"`
}

// Location places the hotel and its walking distance to a landmark.
type Location struct {
	City      string   `json:"city"`
	Country   string   `json:"country"`
	District  string   `json:"district"`
	Latitude  *float64 `json:"latitude"`
	Longitude *float64 `json:"longitude"`
	Landmark  string   `json:"landmark"`
	DistanceM int      `json:"distance_m"`
}

// Hotel is the contracted property and its standing terms.
type Hotel struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	Name         string
	NameAr       string
	Stars        int
	Location     Location
	Contact      Contact
	RoomTypes    []string
	MealPlans    []string
	Currency     string
	Markup       Markup
	ChildPolicy  ChildPolicy
	Cancellation CancellationPolicy
	Notes        string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Normalize trims, defaults and validates; field problems are reported
// together, keyed by JSON field name.
func (h *Hotel) Normalize() error {
	f := fields{}
	h.Name = strings.TrimSpace(h.Name)
	h.NameAr = strings.TrimSpace(h.NameAr)
	h.Notes = strings.TrimSpace(h.Notes)
	h.Currency = strings.ToUpper(strings.TrimSpace(h.Currency))
	if n := utf8.RuneCountInString(h.Name); n < 2 || n > MaxNameLen {
		f.add("name", "2-160 characters")
	}
	if utf8.RuneCountInString(h.NameAr) > MaxNameLen {
		f.add("name_ar", "at most 160 characters")
	}
	if h.Stars < 0 || h.Stars > 5 {
		f.add("stars", "0-5")
	}
	if utf8.RuneCountInString(h.Notes) > MaxNotesLen {
		f.add("notes", "at most 4000 characters")
	}
	if !currencyPattern.MatchString(h.Currency) {
		f.add("currency", "ISO 4217 code")
	}
	h.normalizeLocation(f)
	h.normalizeContact(f)
	h.RoomTypes = normalizeSet(h.RoomTypes, RoomTypes)
	h.MealPlans = normalizeSet(h.MealPlans, MealPlans)
	if len(h.RoomTypes) == 0 {
		f.add("room_types", "choose at least one room type")
	}
	if len(h.MealPlans) == 0 {
		f.add("meal_plans", "choose at least one meal plan")
	}
	if err := h.Markup.Normalize(); err != nil {
		f.add("markup", err.Error())
	}
	if err := h.ChildPolicy.Normalize(); err != nil {
		f.add("child_policy", err.Error())
	}
	if err := h.Cancellation.Normalize(); err != nil {
		f.add("cancellation", err.Error())
	}
	return f.err("invalid hotel")
}

func (h *Hotel) normalizeLocation(f fields) {
	l := &h.Location
	l.City = clip(l.City, 80)
	l.District = clip(l.District, 120)
	l.Country = strings.ToUpper(strings.TrimSpace(l.Country))
	l.Landmark = strings.ToLower(strings.TrimSpace(l.Landmark))
	if l.City == "" {
		f.add("city", "required")
	}
	if l.Country != "" && !countryPattern.MatchString(l.Country) {
		f.add("country", "ISO 3166 alpha-2 code")
	}
	if l.Landmark != "" && !slices.Contains(Landmarks, l.Landmark) {
		f.add("landmark", "unknown landmark")
	}
	if l.DistanceM < 0 || l.DistanceM > MaxDistance {
		f.add("distance_m", "0-100000 metres")
	}
	if (l.Latitude == nil) != (l.Longitude == nil) {
		f.add("latitude", "latitude and longitude go together")
	}
	if l.Latitude != nil && (*l.Latitude < -90 || *l.Latitude > 90) {
		f.add("latitude", "-90..90")
	}
	if l.Longitude != nil && (*l.Longitude < -180 || *l.Longitude > 180) {
		f.add("longitude", "-180..180")
	}
}

func (h *Hotel) normalizeContact(f fields) {
	c := &h.Contact
	c.SalesName = clip(c.SalesName, 120)
	c.SalesPhone = strings.TrimSpace(c.SalesPhone)
	c.SalesEmail = strings.ToLower(strings.TrimSpace(c.SalesEmail))
	c.ReservationsEmail = strings.ToLower(strings.TrimSpace(c.ReservationsEmail))
	if c.SalesPhone != "" && !phonePattern.MatchString(c.SalesPhone) {
		f.add("sales_phone", "invalid phone number")
	}
	if c.SalesEmail != "" && !validEmail(c.SalesEmail) {
		f.add("sales_email", "invalid email")
	}
	if c.ReservationsEmail != "" && !validEmail(c.ReservationsEmail) {
		f.add("reservations_email", "invalid email")
	}
}

// Supports reports whether the hotel sells the room type and meal plan.
func (h *Hotel) Supports(roomType, mealPlan string) bool {
	return slices.Contains(h.RoomTypes, roomType) && slices.Contains(h.MealPlans, mealPlan)
}

// Summary is the list row: the hotel plus today's operating picture.
type Summary struct {
	Hotel
	SeasonName     string
	SeasonKind     string
	FromNet        int64
	RoomsTotal     int
	RoomsSold      int
	NextRelease    *time.Time
	StopSaleToday  bool
	ContractFiles  int
	SeasonsCount   int
	AllotmentCount int
}

// Detail is the full contracting aggregate of one hotel.
type Detail struct {
	Hotel      Hotel
	Seasons    []Season
	Allotments []Allotment
	StopSales  []StopSale
}

// ListFilter narrows the hotel list.
type ListFilter struct {
	BranchID   uuid.UUID
	Query      string
	City       string
	ActiveOnly bool
	Today      time.Time
}

// Repository persists hotels and their contracting children (DIP).
type Repository interface {
	Create(ctx context.Context, h *Hotel) error
	Update(ctx context.Context, h *Hotel) error
	Find(ctx context.Context, id uuid.UUID) (*Hotel, error)
	// FindForUpdate locks the hotel row so season and allotment writes of
	// one hotel serialize.
	FindForUpdate(ctx context.Context, id uuid.UUID) (*Hotel, error)
	List(ctx context.Context, f ListFilter) ([]Summary, error)

	ListSeasons(ctx context.Context, hotelID uuid.UUID) ([]Season, error)
	SaveSeason(ctx context.Context, s *Season) error
	DeleteSeason(ctx context.Context, hotelID, id uuid.UUID) error

	ListAllotments(ctx context.Context, hotelID uuid.UUID) ([]Allotment, error)
	FindAllotment(ctx context.Context, hotelID, id uuid.UUID) (*Allotment, error)
	SaveAllotment(ctx context.Context, a *Allotment) error
	DeleteAllotment(ctx context.Context, hotelID, id uuid.UUID) error

	ListStopSales(ctx context.Context, hotelID uuid.UUID) ([]StopSale, error)
	CreateStopSale(ctx context.Context, s *StopSale) error
	DeleteStopSale(ctx context.Context, hotelID, id uuid.UUID) error
}

type fields map[string]any

func (f fields) add(key, msg string) {
	if _, ok := f[key]; !ok {
		f[key] = msg
	}
}

func (f fields) err(msg string) error {
	if len(f) == 0 {
		return nil
	}
	e := shared.NewValidation(msg)
	e.Details = map[string]any(f)
	return e
}

func normalizeSet(in []string, allowed []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if slices.Contains(allowed, v) && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return slices.Index(allowed, a) - slices.Index(allowed, b) })
	return out
}

func validEmail(s string) bool {
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

func clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}
