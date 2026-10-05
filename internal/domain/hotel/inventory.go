package hotel

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const dayLayout = "2006-01-02"

// MaxSpanDays bounds seasons, allotments and stop sales to two years.
const MaxSpanDays = 731

// ParseDay reads a YYYY-MM-DD calendar day as UTC midnight.
func ParseDay(s string) (time.Time, error) {
	return time.Parse(dayLayout, strings.TrimSpace(s))
}

// FormatDay renders a calendar day.
func FormatDay(t time.Time) string { return t.Format(dayLayout) }

// Day truncates an instant to its calendar day in loc, as UTC midnight.
func Day(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DaysBetween counts calendar days from a to b (negative when b is earlier).
func DaysBetween(a, b time.Time) int {
	return int(b.Sub(a).Hours() / 24)
}

func validRange(f fields, start, end time.Time) {
	if start.IsZero() || end.IsZero() {
		f.add("start_date", "start_date and end_date are required")
		return
	}
	if end.Before(start) {
		f.add("end_date", "must not be before start_date")
	}
	if DaysBetween(start, end) > MaxSpanDays {
		f.add("end_date", "range is limited to two years")
	}
}

func covers(start, end, day time.Time) bool {
	return !day.Before(start) && !day.After(end)
}

// Season kinds.
const (
	SeasonLow      = "low"
	SeasonShoulder = "shoulder"
	SeasonHigh     = "high"
	SeasonPeak     = "peak"
)

var SeasonKinds = []string{SeasonLow, SeasonShoulder, SeasonHigh, SeasonPeak}

// Rate is one row of the net rate matrix, per night: Single is per room,
// Double/Triple/Quad are per person sharing.
type Rate struct {
	RoomType string `json:"room_type"`
	MealPlan string `json:"meal_plan"`
	Single   int64  `json:"single"`
	Double   int64  `json:"double"`
	Triple   int64  `json:"triple"`
	Quad     int64  `json:"quad"`
}

// PerPerson is the per-person rate for an occupancy of 1-4 adults.
func (r Rate) PerPerson(adults int) int64 {
	switch adults {
	case 1:
		return r.Single
	case 2:
		return r.Double
	case 3:
		return r.Triple
	case 4:
		return r.Quad
	default:
		return 0
	}
}

// AdultReference is the per-person rate child percentages refer to.
func (r Rate) AdultReference() int64 {
	if r.Double > 0 {
		return r.Double
	}
	return r.Single
}

// Season is a validity period with its own net matrix; prices switch
// automatically by stay date. Markup, when set, overrides the hotel's.
type Season struct {
	ID        uuid.UUID
	HotelID   uuid.UUID
	Name      string
	Kind      string
	Start     time.Time
	End       time.Time
	Markup    *Markup
	Rates     []Rate
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Normalize validates the season against the hotel's room types and meal plans.
func (s *Season) Normalize(h *Hotel) error {
	f := fields{}
	s.Kind = strings.ToLower(strings.TrimSpace(s.Kind))
	if !slices.Contains(SeasonKinds, s.Kind) {
		f.add("kind", "low, shoulder, high or peak")
	}
	s.Name = clip(s.Name, 80)
	if s.Name == "" {
		f.add("name", "required")
	}
	validRange(f, s.Start, s.End)
	if s.Markup != nil {
		if err := s.Markup.Normalize(); err != nil {
			f.add("markup", err.Error())
		}
	}
	if len(s.Rates) == 0 {
		f.add("rates", "add at least one rate")
	}
	seen := map[string]bool{}
	for i := range s.Rates {
		r := &s.Rates[i]
		r.RoomType = strings.ToLower(strings.TrimSpace(r.RoomType))
		r.MealPlan = strings.ToLower(strings.TrimSpace(r.MealPlan))
		key := r.RoomType + "/" + r.MealPlan
		switch {
		case !h.Supports(r.RoomType, r.MealPlan):
			f.add("rates", key+" is not offered by the hotel")
		case seen[key]:
			f.add("rates", key+" is listed twice")
		case r.Single < 0 || r.Double < 0 || r.Triple < 0 || r.Quad < 0 ||
			r.Single > MaxMoneyAmount || r.Double > MaxMoneyAmount || r.Triple > MaxMoneyAmount || r.Quad > MaxMoneyAmount:
			f.add("rates", key+" has an amount out of range")
		case r.Single == 0 && r.Double == 0 && r.Triple == 0 && r.Quad == 0:
			f.add("rates", key+" needs at least one price")
		}
		seen[key] = true
	}
	slices.SortFunc(s.Rates, func(a, b Rate) int {
		if d := slices.Index(RoomTypes, a.RoomType) - slices.Index(RoomTypes, b.RoomType); d != 0 {
			return d
		}
		return slices.Index(MealPlans, a.MealPlan) - slices.Index(MealPlans, b.MealPlan)
	})
	return f.err("invalid season")
}

func (s *Season) Covers(day time.Time) bool { return covers(s.Start, s.End, day) }

func (s *Season) Overlaps(o *Season) bool {
	return s.ID != o.ID && !s.End.Before(o.Start) && !o.End.Before(s.Start)
}

// RateFor finds the matrix row for a room type and meal plan.
func (s *Season) RateFor(roomType, mealPlan string) (Rate, bool) {
	for _, r := range s.Rates {
		if r.RoomType == roomType && r.MealPlan == mealPlan {
			return r, true
		}
	}
	return Rate{}, false
}

// MarkupFor is the season override or the hotel default.
func (s *Season) MarkupFor(h *Hotel) Markup {
	if s.Markup != nil {
		return *s.Markup
	}
	return h.Markup
}

// EnsureNoOverlap rejects a season whose dates collide with another one.
func EnsureNoOverlap(candidate *Season, existing []Season) error {
	for i := range existing {
		if candidate.Overlaps(&existing[i]) {
			e := shared.NewConflict("season dates overlap " + existing[i].Name)
			e.Details = map[string]any{"start_date": "overlaps " + existing[i].Name}
			return e
		}
	}
	return nil
}

// SeasonOn finds the season covering day.
func SeasonOn(seasons []Season, day time.Time) (*Season, bool) {
	for i := range seasons {
		if seasons[i].Covers(day) {
			return &seasons[i], true
		}
	}
	return nil, false
}

// Allotment kinds.
const (
	AllotGuaranteed = "guaranteed"
	AllotOnRequest  = "on_request"
)

// Allotment statuses (computed).
const (
	AllotOpen     = "open"
	AllotReleased = "released"
	AllotSoldOut  = "sold_out"
	AllotExpired  = "expired"
)

const (
	MaxAllotRooms  = 10_000
	MaxReleaseDays = 90
)

// Allotment is a block of rooms held at the hotel for a date range.
// Guaranteed rooms sell without asking; unsold ones go back to the hotel
// ReleaseDays before the block starts. On-request rooms need the hotel's
// confirmation for every sale.
type Allotment struct {
	ID          uuid.UUID
	HotelID     uuid.UUID
	RoomType    string
	Kind        string
	Start       time.Time
	End         time.Time
	Rooms       int
	Sold        int
	ReleaseDays int
	Notes       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (a *Allotment) Normalize(h *Hotel) error {
	f := fields{}
	a.RoomType = strings.ToLower(strings.TrimSpace(a.RoomType))
	a.Kind = strings.ToLower(strings.TrimSpace(a.Kind))
	a.Notes = clip(a.Notes, 500)
	if !slices.Contains(h.RoomTypes, a.RoomType) {
		f.add("room_type", "not offered by the hotel")
	}
	if a.Kind != AllotGuaranteed && a.Kind != AllotOnRequest {
		f.add("kind", "guaranteed or on_request")
	}
	validRange(f, a.Start, a.End)
	if a.Rooms < 1 || a.Rooms > MaxAllotRooms {
		f.add("rooms", "1-10000")
	}
	if a.Sold < 0 || a.Sold > a.Rooms {
		f.add("sold", "0 up to rooms")
	}
	if a.ReleaseDays < 0 || a.ReleaseDays > MaxReleaseDays {
		f.add("release_days", "0-90")
	}
	if a.Kind == AllotOnRequest {
		a.ReleaseDays = 0
	}
	return f.err("invalid allotment")
}

// ReleaseDate is the day unsold guaranteed rooms go back to the hotel.
func (a *Allotment) ReleaseDate() time.Time { return a.Start.AddDate(0, 0, -a.ReleaseDays) }

func (a *Allotment) Status(today time.Time) string {
	switch {
	case today.After(a.End):
		return AllotExpired
	case a.Sold >= a.Rooms:
		return AllotSoldOut
	case a.Kind == AllotGuaranteed && !today.Before(a.ReleaseDate()):
		return AllotReleased
	default:
		return AllotOpen
	}
}

// Available is how many rooms can still be sold from the block today.
func (a *Allotment) Available(today time.Time) int {
	if a.Status(today) != AllotOpen {
		return 0
	}
	return a.Rooms - a.Sold
}

// AdjustSold records sales (positive) or returns (negative).
func (a *Allotment) AdjustSold(delta int, now time.Time) error {
	if delta == 0 {
		return shared.NewValidation("delta must not be zero")
	}
	next := a.Sold + delta
	if next < 0 {
		return shared.NewInvalidState("cannot return more rooms than were sold")
	}
	if next > a.Rooms {
		return shared.NewInvalidState("not enough rooms left in the allotment")
	}
	a.Sold = next
	a.UpdatedAt = now
	return nil
}

func (a *Allotment) covers(day time.Time, roomType string) bool {
	return a.RoomType == roomType && covers(a.Start, a.End, day)
}

// StopSale closes sales for a date range, for one room type or ("") all.
type StopSale struct {
	ID        uuid.UUID
	HotelID   uuid.UUID
	Start     time.Time
	End       time.Time
	RoomType  string
	Reason    string
	CreatedBy *uuid.UUID
	CreatedAt time.Time
}

func (s *StopSale) Normalize(h *Hotel) error {
	f := fields{}
	s.RoomType = strings.ToLower(strings.TrimSpace(s.RoomType))
	s.Reason = clip(s.Reason, 300)
	if s.RoomType != "" && !slices.Contains(h.RoomTypes, s.RoomType) {
		f.add("room_type", "not offered by the hotel")
	}
	validRange(f, s.Start, s.End)
	return f.err("invalid stop sale")
}

// Covers reports whether the stop sale blocks roomType on day.
func (s *StopSale) Covers(day time.Time, roomType string) bool {
	return (s.RoomType == "" || s.RoomType == roomType) && covers(s.Start, s.End, day)
}
