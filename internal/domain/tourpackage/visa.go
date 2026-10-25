package tourpackage

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// KindVisa is a desk product: visa types with company and selling prices.
const KindVisa = "visa"

// VisaOpenCode is the standing intake slot seeded on a new visa sheet.
const VisaOpenCode = "OPEN"

// IsVisa reports a visa-only product sheet (not an Umrah / Hajj / tour).
func IsVisa(kind string) bool { return kind == KindVisa }

// VisaTypes are the Nusuk Masar enums a visa sheet may price.
func VisaTypes() []string { return slices.Clone(visaTypes) }

// VisaOffer is one visa type the desk sells from this sheet.
type VisaOffer struct {
	Type            string `json:"type"`
	Net             int64  `json:"net"`
	Sell            int64  `json:"sell"`
	ProcessingDays  int    `json:"processing_days"`
	HealthInsurance bool   `json:"health_insurance"`
	Notes           string `json:"notes"`
}

// CheapestVisaSell is the lowest selling price, if any offer is priced.
func CheapestVisaSell(offers []VisaOffer) (int64, bool) {
	var best int64
	ok := false
	for _, o := range offers {
		if o.Sell <= 0 {
			continue
		}
		if !ok || o.Sell < best {
			best, ok = o.Sell, true
		}
	}
	return best, ok
}

// FindVisaOffer returns the offer for type, or false when the sheet does not sell it.
func FindVisaOffer(offers []VisaOffer, typ string) (VisaOffer, bool) {
	typ = strings.ToUpper(strings.TrimSpace(typ))
	for _, o := range offers {
		if o.Type == typ {
			return o, true
		}
	}
	return VisaOffer{}, false
}

// NormalizeVisaOffers trims, dedupes and validates priced visa types.
func NormalizeVisaOffers(in []VisaOffer) ([]VisaOffer, error) {
	seen := map[string]struct{}{}
	out := make([]VisaOffer, 0, len(in))
	for _, o := range in {
		o.Type = strings.ToUpper(strings.TrimSpace(o.Type))
		o.Notes = clip(o.Notes, 500)
		if o.Type == "" && o.Net == 0 && o.Sell == 0 && o.Notes == "" && !o.HealthInsurance && o.ProcessingDays == 0 {
			continue
		}
		if o.Type == "" || !slices.Contains(visaTypes, o.Type) {
			return nil, shared.NewValidation("visa offer type must be one of " + strings.Join(visaTypes, ", "))
		}
		if _, dup := seen[o.Type]; dup {
			return nil, shared.NewValidation("visa offer type " + o.Type + " appears twice")
		}
		if o.Net < 0 || o.Sell < 0 {
			return nil, shared.NewValidation("visa offer prices cannot be negative")
		}
		if o.ProcessingDays < 0 || o.ProcessingDays > 90 {
			return nil, shared.NewValidation("visa offer processing_days must be between 0 and 90")
		}
		seen[o.Type] = struct{}{}
		out = append(out, o)
		if len(out) == len(visaTypes) {
			break
		}
	}
	return out, nil
}

// ApplyVisaKind strips pilgrimage and custom-tour fields a visa sheet does not sell.
func ApplyVisaKind(s *Spec) {
	s.Kit = nil
	s.Ziyarat = Ziyarat{}
	s.Included = nil
	s.Services = nil
	s.Activities = nil
	s.Destination = Destination{}
	s.Stays = nil
	s.Guidance.FemaleGuide = false
	s.Nights = Nights{}
	s.Makkah = Hotel{}
	s.Madinah = Hotel{}
	s.Itinerary = nil
}

// SyncVisaHeader mirrors the first priced offer onto the single visa summary field.
func SyncVisaHeader(s *Spec) {
	if len(s.VisaOffers) == 0 {
		s.Visa = Visa{}
		return
	}
	s.Visa.Type = s.VisaOffers[0].Type
	s.Visa.HealthInsurance = false
	for _, o := range s.VisaOffers {
		if o.HealthInsurance {
			s.Visa.HealthInsurance = true
			return
		}
	}
}

// StandingVisaDeparture is the open intake slot so a visa booking can attach
// without a pilgrimage departure date.
func StandingVisaDeparture(packageID uuid.UUID, capacity int, currency string, durationDays int, now time.Time) *Departure {
	if capacity <= 0 {
		capacity = 9999
	}
	days := durationDays
	if days < 1 {
		days = 14
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if currency == "" {
		currency = DefaultPackageCurrency
	}
	return &Departure{
		ID:               uuid.New(),
		PackageID:        packageID,
		Code:             VisaOpenCode,
		DepartDate:       day,
		ReturnDate:       day.AddDate(0, 0, days),
		CapacityTotal:    capacity,
		AllowOversell:    true,
		IsActive:         true,
		Currency:         currency,
		SoftThresholdPct: 80,
		CreatedAt:        now.UTC(),
		UpdatedAt:        now.UTC(),
	}
}
