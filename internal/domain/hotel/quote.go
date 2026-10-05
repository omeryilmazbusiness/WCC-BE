package hotel

import (
	"slices"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	MaxQuoteNights   = 60
	MaxQuoteRooms    = 50
	MaxRoomAdults    = 4
	MaxRoomChildren  = 4
	MaxChildAge      = 17
	AvailInstant     = "instant"
	AvailOnRequest   = "on_request"
	AvailStopSale    = "stop_sale"
	AvailUnavailable = "unavailable"
)

// Child is a child guest sharing the room; Bed asks for its own bed.
type Child struct {
	Age int  `json:"age"`
	Bed bool `json:"bed"`
}

// QuoteRequest prices Rooms identical rooms for a stay.
type QuoteRequest struct {
	CheckIn  time.Time
	CheckOut time.Time
	RoomType string
	MealPlan string
	Rooms    int
	Adults   int
	Children []Child
	ExtraBed bool
	// Today drives allotment release and the cancellation outlook.
	Today time.Time
}

// QuoteNight is one night of the stay, for all rooms.
type QuoteNight struct {
	Date       string `json:"date"`
	SeasonName string `json:"season_name"`
	SeasonKind string `json:"season_kind"`
	Net        int64  `json:"net"`
	Gross      int64  `json:"gross"`
	Priced     bool   `json:"priced"`
	StopSale   bool   `json:"stop_sale"`
}

// ChildLine shows how a child was priced (per room, per night, net).
type ChildLine struct {
	Age  int    `json:"age"`
	Band string `json:"band"`
	Bed  bool   `json:"bed"`
	Net  int64  `json:"net"`
}

// PenaltyLine prices one cancellation tier for this stay (gross).
type PenaltyLine struct {
	MinDays int    `json:"min_days"`
	Kind    string `json:"kind"`
	Value   int    `json:"value"`
	Amount  int64  `json:"amount"`
	From    string `json:"from"`
}

// CancellationOutlook is the policy priced for this stay.
type CancellationOutlook struct {
	FreeUntil    string        `json:"free_until"`
	FreeNow      bool          `json:"free_now"`
	PenaltyToday int64         `json:"penalty_today"`
	Tiers        []PenaltyLine `json:"tiers"`
	NoShow       int64         `json:"no_show"`
}

// Quote is the priced stay.
type Quote struct {
	Currency      string              `json:"currency"`
	CheckIn       string              `json:"check_in"`
	CheckOut      string              `json:"check_out"`
	Nights        int                 `json:"nights"`
	Rooms         int                 `json:"rooms"`
	Adults        int                 `json:"adults"`
	Guests        int                 `json:"guests"`
	RoomType      string              `json:"room_type"`
	MealPlan      string              `json:"meal_plan"`
	NetTotal      int64               `json:"net_total"`
	GrossTotal    int64               `json:"gross_total"`
	Profit        int64               `json:"profit"`
	Bookable      bool                `json:"bookable"`
	Availability  string              `json:"availability"`
	AllotmentLeft int                 `json:"allotment_left"`
	MissingDates  []string            `json:"missing_dates"`
	StopSaleDates []string            `json:"stop_sale_dates"`
	Children      []ChildLine         `json:"children"`
	NightsDetail  []QuoteNight        `json:"nights_detail"`
	Cancellation  CancellationOutlook `json:"cancellation"`
}

func (r *QuoteRequest) normalize(h *Hotel) error {
	f := fields{}
	nights := DaysBetween(r.CheckIn, r.CheckOut)
	switch {
	case r.CheckIn.IsZero() || r.CheckOut.IsZero():
		f.add("check_in", "check_in and check_out are required")
	case nights < 1:
		f.add("check_out", "must be after check_in")
	case nights > MaxQuoteNights:
		f.add("check_out", "stays are limited to 60 nights")
	}
	if r.Rooms == 0 {
		r.Rooms = 1
	}
	if r.Rooms < 1 || r.Rooms > MaxQuoteRooms {
		f.add("rooms", "1-50")
	}
	if !h.Supports(r.RoomType, r.MealPlan) {
		f.add("room_type", "room type or meal plan not offered")
	}
	if len(r.Children) > MaxRoomChildren {
		f.add("children", "at most 4 per room")
	}
	kids := r.Children[:0:0]
	for _, c := range r.Children {
		if c.Age < 0 || c.Age > MaxChildAge {
			f.add("children", "ages are 0-17")
			continue
		}
		if h.ChildPolicy.Band(c.Age) == BandAdult {
			r.Adults++
			continue
		}
		kids = append(kids, c)
	}
	r.Children = kids
	if r.Adults < 1 || r.Adults > MaxRoomAdults {
		f.add("adults", "1-4 per room, counting children priced as adults")
	}
	return f.err("invalid quote request")
}

// Price quotes a stay from the hotel's seasons, allotments and stop sales.
func Price(h *Hotel, seasons []Season, allotments []Allotment, stops []StopSale, req QuoteRequest) (*Quote, error) {
	if !h.IsActive {
		return nil, shared.NewInvalidState("hotel is inactive")
	}
	if err := req.normalize(h); err != nil {
		return nil, err
	}
	q := &Quote{
		Currency: h.Currency, CheckIn: FormatDay(req.CheckIn), CheckOut: FormatDay(req.CheckOut),
		Rooms: req.Rooms, Adults: req.Adults, RoomType: req.RoomType, MealPlan: req.MealPlan,
		MissingDates: []string{}, StopSaleDates: []string{}, Children: []ChildLine{}, NightsDetail: []QuoteNight{},
	}
	q.Guests = (req.Adults + len(req.Children)) * req.Rooms
	if req.ExtraBed {
		q.Guests += req.Rooms
	}
	nightlyGross := []int64{}
	left := -1
	for day := req.CheckIn; day.Before(req.CheckOut); day = day.AddDate(0, 0, 1) {
		n := priceNight(h, seasons, req, day, q)
		if slices.ContainsFunc(stops, func(s StopSale) bool { return s.Covers(day, req.RoomType) }) {
			n.StopSale = true
			q.StopSaleDates = append(q.StopSaleDates, n.Date)
		}
		if !n.Priced {
			q.MissingDates = append(q.MissingDates, n.Date)
		}
		q.NetTotal += n.Net
		q.GrossTotal += n.Gross
		nightlyGross = append(nightlyGross, n.Gross)
		q.NightsDetail = append(q.NightsDetail, n)
		if a := guaranteedLeft(allotments, day, req.RoomType, req.Today); left < 0 || a < left {
			left = a
		}
	}
	q.Nights = len(q.NightsDetail)
	q.Profit = q.GrossTotal - q.NetTotal
	q.AllotmentLeft = max(left, 0)
	q.Bookable = len(q.MissingDates) == 0 && len(q.StopSaleDates) == 0
	switch {
	case len(q.StopSaleDates) > 0:
		q.Availability = AvailStopSale
	case len(q.MissingDates) > 0:
		q.Availability = AvailUnavailable
	case q.AllotmentLeft >= req.Rooms:
		q.Availability = AvailInstant
	default:
		q.Availability = AvailOnRequest
	}
	q.Cancellation = outlook(h.Cancellation, req.CheckIn, req.Today, nightlyGross)
	return q, nil
}

func priceNight(h *Hotel, seasons []Season, req QuoteRequest, day time.Time, q *Quote) QuoteNight {
	n := QuoteNight{Date: FormatDay(day)}
	season, ok := SeasonOn(seasons, day)
	if !ok {
		return n
	}
	n.SeasonName, n.SeasonKind = season.Name, season.Kind
	rate, ok := season.RateFor(req.RoomType, req.MealPlan)
	if !ok {
		return n
	}
	pp := rate.PerPerson(req.Adults)
	if pp <= 0 {
		return n
	}
	roomNet := pp * int64(req.Adults)
	paying := req.Adults
	if req.ExtraBed {
		roomNet += h.ChildPolicy.ExtraBedAdult
		paying++
	}
	recordKids := len(q.Children) == 0
	for _, c := range req.Children {
		band, net, _ := h.ChildPolicy.ChildCharge(c.Age, c.Bed, rate.AdultReference())
		roomNet += net
		if net > 0 {
			paying++
		}
		if recordKids {
			q.Children = append(q.Children, ChildLine{Age: c.Age, Band: band, Bed: c.Bed, Net: net})
		}
	}
	rooms := int64(req.Rooms)
	n.Priced = true
	n.Net = roomNet * rooms
	n.Gross = season.MarkupFor(h).Apply(roomNet, paying) * rooms
	return n
}

func guaranteedLeft(allotments []Allotment, day time.Time, roomType string, today time.Time) int {
	left := 0
	for i := range allotments {
		a := &allotments[i]
		if a.Kind == AllotGuaranteed && a.covers(day, roomType) {
			left += a.Available(today)
		}
	}
	return left
}

func outlook(p CancellationPolicy, checkIn, today time.Time, nightly []int64) CancellationOutlook {
	days := DaysBetween(today, checkIn)
	o := CancellationOutlook{
		FreeUntil:    FormatDay(p.FreeUntil(checkIn)),
		FreeNow:      p.TierFor(days) == nil,
		PenaltyToday: p.Penalty(days, nightly),
		NoShow:       p.NoShowPenalty(nightly),
		Tiers:        make([]PenaltyLine, 0, len(p.Tiers)),
	}
	for _, t := range p.Tiers {
		o.Tiers = append(o.Tiers, PenaltyLine{
			MinDays: t.MinDays, Kind: t.Kind, Value: t.Value, Amount: t.charge(nightly),
			From: FormatDay(checkIn.AddDate(0, 0, -(firstFreeBelow(p, t.MinDays) - 1))),
		})
	}
	return o
}

// firstFreeBelow is the exclusive upper day bound of the tier starting at minDays.
func firstFreeBelow(p CancellationPolicy, minDays int) int {
	upper := p.FreeDays
	for _, t := range p.Tiers {
		if t.MinDays > minDays && t.MinDays < upper {
			upper = t.MinDays
		}
	}
	return upper
}
