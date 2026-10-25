package tourpackage

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Brochure languages. Each download is one locale so RTL and copy stay consistent.
const (
	BrochureEN BrochureLang = "en"
	BrochureAR BrochureLang = "ar"
)

type BrochureLang string

// ParseBrochureLang accepts en/ar (default en).
func ParseBrochureLang(raw string) (BrochureLang, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "en", "eng", "english":
		return BrochureEN, nil
	case "ar", "ara", "arabic":
		return BrochureAR, nil
	default:
		return "", shared.NewValidation("lang must be en or ar")
	}
}

// BrochureBrand is the company face printed on the leaflet.
type BrochureBrand struct {
	NameEN   string
	NameAR   string
	Logo     []byte
	LogoType string
}

func (b BrochureBrand) Name(lang BrochureLang) string {
	if lang == BrochureAR && strings.TrimSpace(b.NameAR) != "" {
		return strings.TrimSpace(b.NameAR)
	}
	if name := strings.TrimSpace(b.NameEN); name != "" {
		return name
	}
	return strings.TrimSpace(b.NameAR)
}

// Brochure is the printable marketing leaflet for one package and locale.
// The PDF adapter renders this; nothing here knows about PDF libraries.
type Brochure struct {
	Lang        BrochureLang
	RTL         bool
	Accent      BrochureAccent
	BrandName   string
	Logo        []byte
	LogoType    string
	Code        string
	Title       string
	Subtitle    string
	Tagline     string
	Kind        string
	Category    string
	Description string
	FromPrice   string
	Facts       []BrochureFact
	Stays       []BrochureStay
	Travel      []BrochureFact
	Services    []BrochureItem
	Programme   []BrochureItem
	Itinerary   []BrochureDay
	Prices      []BrochurePrice
	Prepare     []string
	Excluded    []string
	Chapters    []BrochureChapter
	Features    []BrochureFeature
	Labels      BrochureLabels
	Footer      string
	Filename    string
}

// BrochureFeature is one visual card: a single service or trait of the package.
type BrochureFeature struct {
	Icon, Title, Detail, Tone string
}

// BrochureChapter is one numbered stop on the vertical journey map.
type BrochureChapter struct {
	Number int
	Icon   string
	Title  string
	Body   string
	Notes  []string
}

// BrochureLabels are the leaflet headings for the chosen locale.
type BrochureLabels struct {
	Official, From, PerPerson                          string
	Stay, Travel, Included, Programme, Itinerary       string
	Prices, Excluded, Prepare, Journey, Page, Features string
}

type BrochureAccent struct {
	R, G, B             int
	DeepR, DeepG, DeepB int
	SoftR, SoftG, SoftB int
}

type BrochureFact struct {
	Icon  string
	Label string
	Value string
}

type BrochureStay struct {
	City, Hotel, Board, Access string
	Stars, Nights, DistanceM   int
}

type BrochureItem struct {
	Title, Details string
}

type BrochureDay struct {
	Day                  int
	City, Title, Details string
}

type BrochurePrice struct {
	Label, Amount string
}

// BrochureRenderer turns a leaflet into PDF bytes (DIP).
type BrochureRenderer interface {
	Render(doc Brochure) ([]byte, error)
}

var brochureFileSafe = regexp.MustCompile(`[^A-Z0-9]+`)

// BrochureFilename is the ASCII download name: UMR-2026-ECO-01-brochure-en.pdf
func BrochureFilename(code string, lang BrochureLang) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.Trim(brochureFileSafe.ReplaceAllString(code, "-"), "-")
	if code == "" {
		code = "PACKAGE"
	}
	return fmt.Sprintf("%s-brochure-%s.pdf", code, lang)
}

// BuildBrochure maps a product sheet to a marketing leaflet. Empty optional
// blocks are omitted so Other tours never show Umrah visa / kit / ziyarat.
func BuildBrochure(p Package, tiers []PricingTier, brand BrochureBrand, lang BrochureLang) Brochure {
	if lang != BrochureAR {
		lang = BrochureEN
	}
	cp := copyFor(lang)
	title := strings.TrimSpace(p.NameEN)
	sub := strings.TrimSpace(p.NameAR)
	if lang == BrochureAR {
		if ar := strings.TrimSpace(p.NameAR); ar != "" {
			title, sub = ar, strings.TrimSpace(p.NameEN)
		}
	}
	accent := accentFor(p.Kind)
	currency := p.BaseCurrency
	if currency == "" {
		currency = DefaultPackageCurrency
	}
	from := p.Stats.FromPrice
	fromCur := p.Stats.FromCurrency
	if fromCur == "" {
		fromCur = currency
	}
	if from <= 0 {
		from, fromCur = cheapestRoom(tiers, currency)
	}
	if from <= 0 {
		if sell, ok := CheapestVisaSell(p.Spec.VisaOffers); ok {
			from, fromCur = sell, currency
		}
	}

	doc := Brochure{
		Lang:        lang,
		RTL:         lang == BrochureAR,
		Accent:      accent,
		BrandName:   brand.Name(lang),
		Logo:        brand.Logo,
		LogoType:    brand.LogoType,
		Code:        p.Code,
		Title:       title,
		Subtitle:    sub,
		Tagline:     cp.tagline(p.Kind),
		Kind:        cp.kind(p.Kind),
		Category:    cp.category(p.Category),
		Description: strings.TrimSpace(p.Description),
		FromPrice:   formatBrochureMoney(from, fromCur),
		Labels:      cp.labels,
		Filename:    BrochureFilename(p.Code, lang),
		Footer:      cp.footer(brand.Name(lang)),
	}

	doc.Facts = brochureFacts(p, cp, lang)
	doc.Stays = brochureStays(p, cp)
	doc.Travel = brochureTravel(p, cp)
	doc.Services, doc.Programme = brochureOffer(p, cp)
	doc.Itinerary = brochureDays(p.Spec.Itinerary)
	if IsVisa(p.Kind) {
		doc.Prices = brochureVisaPrices(p.Spec.VisaOffers, currency, cp)
	} else {
		doc.Prices = brochurePrices(tiers, currency)
	}
	doc.Prepare = brochurePrepare(p, cp)
	doc.Chapters = brochureChapters(p, cp)
	doc.Features = brochureFeatures(p, cp)
	if !IsPilgrimage(p.Kind) {
		doc.Excluded = append([]string{}, p.Spec.Excluded...)
	}
	return doc
}

func cheapestRoom(tiers []PricingTier, fallbackCur string) (int64, string) {
	var best int64
	cur := fallbackCur
	for _, t := range tiers {
		if t.Kind != TierRoom && t.Kind != TierOccupancy {
			continue
		}
		if !t.IsActive || t.Amount <= 0 {
			continue
		}
		if best == 0 || t.Amount < best {
			best = t.Amount
			if t.Currency != "" {
				cur = t.Currency
			}
		}
	}
	return best, cur
}

func brochureFacts(p Package, cp brochureCopy, lang BrochureLang) []BrochureFact {
	out := []BrochureFact{
		{Icon: "calendar", Label: cp.Duration, Value: cp.days(p.DurationDays)},
		{Icon: "plane", Label: cp.Transport, Value: cp.transport(p.TransportMode)},
	}
	if IsPilgrimage(p.Kind) {
		nights := p.Spec.Nights.Makkah + p.Spec.Nights.Madinah
		if nights > 0 {
			out = append(out, BrochureFact{Icon: "moon", Label: cp.Stay, Value: cp.nights(nights)})
		}
	} else if country := strings.TrimSpace(p.Spec.Destination.Country); country != "" {
		place := country
		if d := strings.Join(p.Spec.Destination.Districts, " · "); d != "" {
			place = country + " · " + d
		}
		out = append(out, BrochureFact{Icon: "pin", Label: cp.Destination, Value: place})
	}
	if p.Spec.Guidance.LeaderName != "" {
		out = append(out, BrochureFact{Icon: "guide", Label: cp.Leader, Value: p.Spec.Guidance.LeaderName})
	}
	if len(out) < 4 && p.CapacityTotal > 0 {
		out = append(out, BrochureFact{Icon: "users", Label: cp.Group, Value: fmt.Sprintf("%d", p.CapacityTotal)})
	}
	_ = lang
	return out
}

func brochureStays(p Package, cp brochureCopy) []BrochureStay {
	if !IsPilgrimage(p.Kind) {
		out := make([]BrochureStay, 0, len(p.Spec.Stays))
		for _, s := range p.Spec.Stays {
			if s.Hotel.Name == "" && s.City == "" {
				continue
			}
			out = append(out, BrochureStay{
				City: s.City, Hotel: s.Hotel.Name, Stars: s.Hotel.Stars, Nights: s.Nights,
				Board: cp.board(s.Hotel.Board), Access: cp.access(s.Hotel.Access), DistanceM: s.Hotel.DistanceM,
			})
		}
		return out
	}
	var out []BrochureStay
	for _, city := range []struct {
		key string
		h   Hotel
		n   int
	}{
		{"makkah", p.Spec.Makkah, p.Spec.Nights.Makkah},
		{"madinah", p.Spec.Madinah, p.Spec.Nights.Madinah},
	} {
		if city.h.Name == "" && city.n == 0 {
			continue
		}
		out = append(out, BrochureStay{
			City: cp.city(city.key), Hotel: city.h.Name, Stars: city.h.Stars, Nights: city.n,
			Board: cp.board(city.h.Board), Access: cp.access(city.h.Access), DistanceM: city.h.DistanceM,
		})
	}
	return out
}

func brochureTravel(p Package, cp brochureCopy) []BrochureFact {
	var out []BrochureFact
	f := p.Spec.Flights
	if f.Airline != "" || f.Outbound.Route != "" {
		route := f.Outbound.Route
		if f.Inbound.Route != "" {
			if route != "" {
				route += "  ·  " + f.Inbound.Route
			} else {
				route = f.Inbound.Route
			}
		}
		label := f.Airline
		if f.Routing != "" {
			if label != "" {
				label += " · " + cp.routing(f.Routing)
			} else {
				label = cp.routing(f.Routing)
			}
		}
		if label == "" {
			label = cp.Flights
		}
		out = append(out, BrochureFact{Icon: "plane", Label: label, Value: route})
	}
	if p.Spec.Transfers.Intercity != "" {
		out = append(out, BrochureFact{Icon: "bus", Label: cp.Ground, Value: cp.intercity(p.Spec.Transfers.Intercity)})
	}
	if p.Spec.Bus.Company != "" {
		detail := p.Spec.Bus.Company
		if p.Spec.Bus.Details != "" {
			detail += " · " + p.Spec.Bus.Details
		}
		out = append(out, BrochureFact{Icon: "bus", Label: cp.Coach, Value: detail})
	}
	if IsPilgrimage(p.Kind) && p.Spec.Visa.Type != "" {
		out = append(out, BrochureFact{Icon: "stamp", Label: cp.Visa, Value: cp.visa(p.Spec.Visa.Type)})
	}
	if IsVisa(p.Kind) {
		for _, o := range p.Spec.VisaOffers {
			out = append(out, BrochureFact{Icon: "stamp", Label: cp.visa(o.Type), Value: formatBrochureMoney(o.Sell, p.BaseCurrency)})
		}
	}
	return out
}

func brochureOffer(p Package, cp brochureCopy) (services, programme []BrochureItem) {
	if IsVisa(p.Kind) {
		for _, o := range p.Spec.VisaOffers {
			details := o.Notes
			if o.ProcessingDays > 0 {
				if details != "" {
					details = cp.days(o.ProcessingDays) + " · " + details
				} else {
					details = cp.days(o.ProcessingDays)
				}
			}
			services = append(services, BrochureItem{Title: cp.visa(o.Type), Details: details})
		}
		return services, programme
	}
	if IsPilgrimage(p.Kind) {
		if p.Spec.Visa.HealthInsurance {
			services = append(services, BrochureItem{Title: cp.Insurance})
		}
		for _, k := range p.Spec.Kit {
			services = append(services, BrochureItem{Title: cp.kit(k)})
		}
		for _, z := range p.Spec.Ziyarat.Makkah {
			programme = append(programme, BrochureItem{Title: cp.city("makkah") + " · " + cp.ziyarat(z)})
		}
		for _, z := range p.Spec.Ziyarat.Madinah {
			programme = append(programme, BrochureItem{Title: cp.city("madinah") + " · " + cp.ziyarat(z)})
		}
		for _, line := range p.Spec.Included {
			services = append(services, BrochureItem{Title: line})
		}
		return services, programme
	}
	for _, a := range p.Spec.Services {
		if a.Title == "" {
			continue
		}
		services = append(services, BrochureItem{Title: a.Title, Details: a.Details})
	}
	for _, a := range p.Spec.Activities {
		if a.Title == "" {
			continue
		}
		programme = append(programme, BrochureItem{Title: a.Title, Details: a.Details})
	}
	return services, programme
}

func brochureDays(days []ItineraryDay) []BrochureDay {
	out := make([]BrochureDay, 0, len(days))
	for _, d := range days {
		if d.Title == "" {
			continue
		}
		out = append(out, BrochureDay{Day: d.Day, City: d.City, Title: d.Title, Details: d.Details})
		if len(out) == 21 {
			break
		}
	}
	return out
}

func brochurePrepare(p Package, cp brochureCopy) []string {
	var out []string
	if IsPilgrimage(p.Kind) && p.Spec.Visa.Type != "" {
		out = append(out, cp.visa(p.Spec.Visa.Type))
	}
	if IsPilgrimage(p.Kind) && p.Spec.Visa.HealthInsurance {
		out = append(out, cp.Insurance)
	}
	if p.Spec.Transfers.AirportMeet {
		out = append(out, cp.AirportMeet)
	}
	if p.Spec.Guidance.LeaderName != "" {
		out = append(out, cp.Leader+": "+p.Spec.Guidance.LeaderName)
	}
	if IsPilgrimage(p.Kind) {
		for _, k := range p.Spec.Kit {
			out = append(out, cp.kit(k))
		}
	}
	return out
}

func brochureChapters(p Package, cp brochureCopy) []BrochureChapter {
	var out []BrochureChapter
	add := func(icon, title, body string, notes ...string) {
		clean := notes[:0]
		for _, n := range notes {
			if strings.TrimSpace(n) != "" {
				clean = append(clean, n)
			}
		}
		out = append(out, BrochureChapter{Number: len(out) + 1, Icon: icon, Title: title, Body: body, Notes: append([]string{}, clean...)})
	}

	if IsPilgrimage(p.Kind) {
		airline := strings.TrimSpace(p.Spec.Flights.Airline)
		route := strings.TrimSpace(p.Spec.Flights.Outbound.Route)
		if p.TransportMode == "road" && airline == "" {
			add("bus", cp.ChDepart, narrateDepart(p, cp), p.Spec.Bus.Company, p.Spec.Bus.Details)
		} else {
			add("plane", cp.ChDepart, narrateDepart(p, cp), airline, route, cp.routing(p.Spec.Flights.Routing))
		}
		if p.Spec.Nights.Madinah > 0 || p.Spec.Madinah.Name != "" {
			add("mosque", fmt.Sprintf(cp.ChMadinah, p.Spec.Nights.Madinah),
				narrateStay(p.Spec.Nights.Madinah, p.Spec.Madinah, "madinah", cp.narrMadinah, cp.worshipMadinah, p.Spec.Ziyarat.Madinah, p.Spec.Itinerary, p, cp),
				hotelNotes(p.Spec.Madinah, p.Spec.Nights.Madinah, cp)...)
		}
		if p.Spec.Transfers.Intercity != "" || (p.Spec.Nights.Madinah > 0 && p.Spec.Nights.Makkah > 0) {
			mode := cp.intercity(p.Spec.Transfers.Intercity)
			if mode == "" {
				mode = cp.Coach
			}
			add("bus", cp.ChTransfer, narrateTransfer(p, cp), mode)
		}
		if p.Spec.Nights.Makkah > 0 || p.Spec.Makkah.Name != "" {
			add("kaaba", fmt.Sprintf(cp.ChMakkah, p.Spec.Nights.Makkah),
				narrateStay(p.Spec.Nights.Makkah, p.Spec.Makkah, "makkah", cp.narrMakkah, cp.ritesMakkah, p.Spec.Ziyarat.Makkah, p.Spec.Itinerary, p, cp),
				hotelNotes(p.Spec.Makkah, p.Spec.Nights.Makkah, cp)...)
		}
		ret := strings.TrimSpace(p.Spec.Flights.Inbound.Route)
		add("return", cp.ChReturn, narrateReturn(p, cp), ret)
		return out
	}

	place := strings.TrimSpace(p.Spec.Destination.Country)
	if d := strings.Join(p.Spec.Destination.Districts, " · "); d != "" {
		if place != "" {
			place += " · " + d
		} else {
			place = d
		}
	}
	if place == "" {
		place = cp.Destination
	}
	add("plane", cp.ChArrive, narrateArrive(p, place, cp), place, cp.transport(p.TransportMode))
	for _, s := range p.Spec.Stays {
		if s.City == "" && s.Hotel.Name == "" {
			continue
		}
		city := orHotel(s.City, place)
		add("hotel", fmt.Sprintf(cp.ChCity, city, s.Nights),
			narrateCity(s, place, p.Spec.Itinerary, cp),
			hotelNotes(s.Hotel, s.Nights, cp)...)
	}
	if p.Spec.Bus.Company != "" {
		add("bus", cp.Coach, p.Spec.Bus.Details, p.Spec.Bus.Company)
	}
	for _, a := range p.Spec.Activities {
		if a.Title == "" {
			continue
		}
		body := strings.TrimSpace(a.Details)
		if body == "" {
			body = a.Title
		} else if a.Title != "" {
			body = a.Title + ". " + body
		}
		add("star", a.Title, body)
	}
	add("return", cp.ChReturn, narrateReturn(p, cp))
	return out
}

func orHotel(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return strings.TrimSpace(name)
}

func hotelNotes(h Hotel, nights int, cp brochureCopy) []string {
	var out []string
	if h.Name != "" {
		out = append(out, h.Name)
	}
	if nights > 0 {
		out = append(out, cp.nights(nights))
	}
	if h.Stars > 0 {
		out = append(out, fmt.Sprintf("%d-star", h.Stars))
	}
	if b := cp.board(h.Board); b != "" {
		out = append(out, b)
	}
	if a := cp.access(h.Access); a != "" {
		out = append(out, a)
	}
	if h.DistanceM > 0 {
		out = append(out, fmt.Sprintf("%d m", h.DistanceM))
	}
	return out
}

func brochurePrices(tiers []PricingTier, currency string) []BrochurePrice {
	out := make([]BrochurePrice, 0, len(tiers))
	for _, t := range tiers {
		if !t.IsActive || t.Amount <= 0 {
			continue
		}
		cur := t.Currency
		if cur == "" {
			cur = currency
		}
		label := strings.TrimSpace(t.Label)
		if label == "" {
			label = t.Code
		}
		out = append(out, BrochurePrice{Label: label, Amount: formatBrochureMoney(t.Amount, cur)})
	}
	return out
}

func brochureVisaPrices(offers []VisaOffer, currency string, cp brochureCopy) []BrochurePrice {
	out := make([]BrochurePrice, 0, len(offers))
	for _, o := range offers {
		if o.Sell <= 0 {
			continue
		}
		out = append(out, BrochurePrice{Label: cp.visa(o.Type), Amount: formatBrochureMoney(o.Sell, currency)})
	}
	return out
}

func formatBrochureMoney(minor int64, currency string) string {
	if minor <= 0 || currency == "" {
		return ""
	}
	return shared.FormatMinor(minor, currency)
}

func accentFor(kind string) BrochureAccent {
	_ = kind
	return BrochureAccent{R: 184, G: 149, B: 106, DeepR: 61, DeepG: 58, DeepB: 54, SoftR: 251, SoftG: 248, SoftB: 243}
}
