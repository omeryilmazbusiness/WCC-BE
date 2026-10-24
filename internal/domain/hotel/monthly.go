package hotel

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	MinRateYear = 2020
	MaxRateYear = 2100
)

// MonthlyRate is the nightly firma (net) and satış (sell) price of one room
// type for a calendar month. Occupancy is not split: the desk books the room.
type MonthlyRate struct {
	HotelID  uuid.UUID `json:"hotel_id,omitempty"`
	Year     int       `json:"year"`
	Month    int       `json:"month"`
	RoomType string    `json:"room_type"`
	Net      int64     `json:"net"`
	Sell     int64     `json:"sell"`
}

// NormalizeYearSheet returns the priced cells of one hotel-year.
func NormalizeYearSheet(h *Hotel, year int, rates []MonthlyRate) ([]MonthlyRate, error) {
	f := fields{}
	if year < MinRateYear || year > MaxRateYear {
		f.add("year", "2020-2100")
	}
	seen := map[string]bool{}
	out := make([]MonthlyRate, 0, len(rates))
	for _, r := range rates {
		r.Year = year
		r.HotelID = h.ID
		r.RoomType = strings.ToLower(strings.TrimSpace(r.RoomType))
		if r.Month < 1 || r.Month > 12 {
			f.add("month", "1-12")
			continue
		}
		if !slices.Contains(h.RoomTypes, r.RoomType) {
			f.add("room_type", r.RoomType+" is not offered")
			continue
		}
		if r.Net < 0 || r.Sell < 0 || r.Net > MaxMoneyAmount || r.Sell > MaxMoneyAmount {
			f.add("rates", r.RoomType+" has an amount out of range")
			continue
		}
		if r.Net == 0 && r.Sell == 0 {
			continue
		}
		key := r.RoomType + "/" + strconv.Itoa(r.Month)
		if seen[key] {
			f.add("rates", key+" is listed twice")
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out, f.err("invalid monthly rates")
}

// MonthlyOn finds the rate covering day for a room type.
func MonthlyOn(rates []MonthlyRate, day time.Time, roomType string) (MonthlyRate, bool) {
	y, m, _ := day.Date()
	for _, r := range rates {
		if r.Year == y && r.Month == int(m) && r.RoomType == roomType {
			return r, true
		}
	}
	return MonthlyRate{}, false
}
