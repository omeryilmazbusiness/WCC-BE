package tourpackage_test

import (
	"strings"
	"testing"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

func TestParseBrochureLang(t *testing.T) {
	en, err := domain.ParseBrochureLang("")
	if err != nil || en != domain.BrochureEN {
		t.Fatalf("default: %q %v", en, err)
	}
	ar, err := domain.ParseBrochureLang("AR")
	if err != nil || ar != domain.BrochureAR {
		t.Fatalf("ar: %q %v", ar, err)
	}
	if _, err := domain.ParseBrochureLang("tr"); err == nil {
		t.Fatal("expected invalid lang")
	}
}

func TestBrochureFilename(t *testing.T) {
	if got := domain.BrochureFilename("umr-2026-eco-01", domain.BrochureEN); got != "UMR-2026-ECO-01-brochure-en.pdf" {
		t.Fatalf("got %s", got)
	}
	if got := domain.BrochureFilename("  messy name!! ", domain.BrochureAR); got != "MESSY-NAME-brochure-ar.pdf" {
		t.Fatalf("got %s", got)
	}
}

func TestBuildBrochurePilgrimageEN(t *testing.T) {
	p := domain.Package{
		Code: "UMR-2026-STD-01", NameEN: "Ramadan Umrah", NameAR: "عمرة رمضان",
		Description: "A quiet ten-night stay by the Haram.",
		Header:      domain.Header{Kind: domain.KindUmrah, Category: "standard", DurationDays: 10, TransportMode: "flight_scheduled", CapacityTotal: 45, BaseCurrency: "SAR"},
		Spec: domain.Spec{
			Nights:    domain.Nights{Makkah: 7, Madinah: 3},
			Makkah:    domain.Hotel{Name: "Swissôtel", Stars: 5, Board: "bb", DistanceM: 50, Access: "walking"},
			Madinah:   domain.Hotel{Name: "Pullman", Stars: 5, Board: "fb"},
			Flights:   domain.Flights{Airline: "Saudia", Routing: "direct", Outbound: domain.FlightLeg{Route: "IST-JED"}, Inbound: domain.FlightLeg{Route: "MED-IST"}},
			Visa:      domain.Visa{Type: "UMRAH_VISA", HealthInsurance: true},
			Kit:       []string{"ihram", "zamzam"},
			Ziyarat:   domain.Ziyarat{Makkah: []string{"hira"}, Madinah: []string{"quba"}},
			Included:  []string{"Airport meet & assist"},
			Itinerary: []domain.ItineraryDay{{Day: 1, City: "makkah", Title: "Arrival", Details: "First Umrah"}},
		},
		Stats: domain.PackageStats{FromPrice: 950000, FromCurrency: "SAR"},
	}
	doc := domain.BuildBrochure(p, nil, domain.BrochureBrand{NameEN: "Al Noor Travel", NameAR: "النور للسفر"}, domain.BrochureEN)
	if doc.Title != "Ramadan Umrah" || doc.Kind != "Umrah" || doc.RTL {
		t.Fatalf("hero %+v", doc)
	}
	if doc.BrandName != "Al Noor Travel" || !strings.Contains(doc.Footer, "Al Noor Travel") {
		t.Fatalf("brand %q footer %q", doc.BrandName, doc.Footer)
	}
	if doc.FromPrice != "9500.00 SAR" || doc.Filename != "UMR-2026-STD-01-brochure-en.pdf" {
		t.Fatalf("price/file %q %q", doc.FromPrice, doc.Filename)
	}
	if len(doc.Stays) != 2 || doc.Stays[0].Hotel != "Swissôtel" || doc.Stays[0].City != "Makkah" {
		t.Fatalf("stays %+v", doc.Stays)
	}
	joined := offerTitles(doc)
	for _, need := range []string{"Ihram", "Zamzam", "Health insurance", "Airport meet & assist", "Makkah · Jabal al-Nour (Hira)"} {
		if !strings.Contains(joined, need) {
			t.Fatalf("missing %q in %q", need, joined)
		}
	}
	if len(doc.Itinerary) != 1 || doc.Itinerary[0].Title != "Arrival" {
		t.Fatalf("itinerary %+v", doc.Itinerary)
	}
	if len(doc.Chapters) < 4 {
		t.Fatalf("journey chapters %+v", doc.Chapters)
	}
	if doc.Chapters[0].Icon != "plane" || !strings.Contains(doc.Chapters[0].Body, "Saudia") {
		t.Fatalf("depart %+v", doc.Chapters[0])
	}
	if !strings.Contains(doc.Chapters[0].Body, "IST-JED") {
		t.Fatalf("depart route %q", doc.Chapters[0].Body)
	}
	if !strings.Contains(doc.Chapters[1].Title, "Madinah") || doc.Chapters[1].Icon != "mosque" {
		t.Fatalf("madinah %+v", doc.Chapters[1])
	}
	if !strings.Contains(doc.Chapters[1].Body, "Pullman") || !strings.Contains(doc.Chapters[1].Body, "Quba") {
		t.Fatalf("madinah body %q", doc.Chapters[1].Body)
	}
	if !strings.Contains(doc.Chapters[3].Body, "Swissôtel") {
		t.Fatalf("makkah body %q", doc.Chapters[3].Body)
	}
	shown := offerTitles(doc) + featureTitles(doc)
	for _, need := range []string{"Ihram", "Zamzam", "Umrah visa", "Health insurance", "Quba", "Swissôtel"} {
		if !strings.Contains(shown, need) {
			t.Fatalf("feature missing %q in %q", need, shown)
		}
	}
}

func TestBuildBrochureOtherDropsPilgrimageOffer(t *testing.T) {
	p := domain.Package{
		Code: "CUS-2026-LUX-01", NameEN: "Cappadocia Escape", NameAR: "كابادوكيا",
		Header: domain.Header{Kind: domain.KindOther, Category: "luxury", DurationDays: 6, TransportMode: "road", BaseCurrency: "USD"},
		Spec: domain.Spec{
			Destination: domain.Destination{Country: "Turkey", Districts: []string{"Istanbul", "Cappadocia"}},
			Stays:       []domain.Stay{{City: "Istanbul", Nights: 3, Hotel: domain.Hotel{Name: "Pera Palace", Stars: 5}}},
			Visa:        domain.Visa{Type: "UMRAH_VISA", HealthInsurance: true},
			Kit:         []string{"ihram"},
			Ziyarat:     domain.Ziyarat{Makkah: []string{"hira"}},
			Services:    []domain.Activity{{Title: "Airport transfer", Details: "Private car"}, {Title: "City card"}},
			Activities:  []domain.Activity{{Title: "Balloon", Details: "Sunrise"}},
			Excluded:    []string{"Travel insurance"},
		},
	}
	doc := domain.BuildBrochure(p, []domain.PricingTier{
		{Code: "DOUBLE", Label: "Double", Kind: domain.TierRoom, Amount: 210000, Currency: "USD", IsActive: true},
		{Code: "VIP", Kind: domain.TierRoom, Amount: 1, IsActive: false},
	}, domain.BrochureBrand{NameEN: "Wodi", NameAR: "ودي"}, domain.BrochureAR)
	if doc.Title != "كابادوكيا" || !doc.RTL || doc.Kind != "رحلة خاصة" {
		t.Fatalf("ar hero %+v", doc)
	}
	if doc.BrandName != "ودي" {
		t.Fatalf("ar brand %q", doc.BrandName)
	}
	if !strings.Contains(factValues(doc), "Turkey · Istanbul · Cappadocia") {
		t.Fatalf("destination facts %+v", doc.Facts)
	}
	if len(doc.Stays) != 1 || doc.Stays[0].Hotel != "Pera Palace" {
		t.Fatalf("stays %+v", doc.Stays)
	}
	joined := offerTitles(doc)
	for _, banned := range []string{"Ihram", "إحرام", "UMRAH", "عمرة", "Hira", "حراء", "تأمين صحي"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("other leaflet leaked pilgrimage offer %q in %q", banned, joined)
		}
	}
	if len(doc.Services) != 2 || doc.Services[0].Title != "Airport transfer" {
		t.Fatalf("services %+v", doc.Services)
	}
	if len(doc.Programme) != 1 || doc.Programme[0].Title != "Balloon" {
		t.Fatalf("programme %+v", doc.Programme)
	}
	if len(doc.Excluded) != 1 || doc.Prices[0].Label != "Double" {
		t.Fatalf("excluded/prices %+v %+v", doc.Excluded, doc.Prices)
	}
	shown := featureTitles(doc)
	for _, banned := range []string{"Ihram", "إحرام", "Hira", "حراء"} {
		if strings.Contains(shown, banned) {
			t.Fatalf("other features leaked pilgrimage %q in %q", banned, shown)
		}
	}
	if !strings.Contains(shown, "Airport transfer") || !strings.Contains(shown, "Balloon") {
		t.Fatalf("other features %q", shown)
	}
	if len(doc.Chapters) < 3 {
		t.Fatalf("other chapters %+v", doc.Chapters)
	}
	if doc.Chapters[0].Icon != "plane" || !strings.Contains(doc.Chapters[1].Title, "Istanbul") {
		t.Fatalf("other journey %+v", doc.Chapters)
	}
	for _, s := range doc.Prepare {
		if strings.Contains(s, "Umrah") || strings.Contains(s, "Ihram") || strings.Contains(s, "إحرام") {
			t.Fatalf("other prepare leaked pilgrimage %q", s)
		}
	}
}

func featureTitles(doc domain.Brochure) string {
	var b strings.Builder
	for _, f := range doc.Features {
		b.WriteString(f.Title)
		b.WriteByte(' ')
		b.WriteString(f.Detail)
		b.WriteByte(' ')
	}
	return b.String()
}

func offerTitles(doc domain.Brochure) string {
	var b strings.Builder
	for _, s := range doc.Services {
		b.WriteString(s.Title)
		b.WriteByte(' ')
	}
	for _, s := range doc.Programme {
		b.WriteString(s.Title)
		b.WriteByte(' ')
	}
	return b.String()
}

func factValues(doc domain.Brochure) string {
	var b strings.Builder
	for _, f := range doc.Facts {
		b.WriteString(f.Value)
		b.WriteByte(' ')
	}
	return b.String()
}
