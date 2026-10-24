package pdf

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

func TestRendererProducesPDF(t *testing.T) {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 5, G: 150, B: 105, A: 255})
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	doc := domain.BuildBrochure(domain.Package{
		Code: "UMR-2026-STD-01", NameEN: "Ramadan Umrah", NameAR: "عمرة رمضان",
		Description: "Ten quiet nights by the Haram.",
		Header:      domain.Header{Kind: domain.KindUmrah, Category: "standard", DurationDays: 10, TransportMode: "flight_scheduled", BaseCurrency: "SAR"},
		Spec: domain.Spec{
			Nights:  domain.Nights{Makkah: 7, Madinah: 3},
			Makkah:  domain.Hotel{Name: "Swissotel", Stars: 5, Board: "bb", Access: "walking", DistanceM: 50},
			Flights: domain.Flights{Airline: "Saudia", Routing: "direct", Outbound: domain.FlightLeg{Route: "IST-JED"}},
			Visa:    domain.Visa{Type: "UMRAH_VISA", HealthInsurance: true},
			Kit:     []string{"ihram", "zamzam"},
			Itinerary: []domain.ItineraryDay{
				{Day: 1, City: "makkah", Title: "Arrival", Details: "First Umrah"},
				{Day: 10, Title: "Departure"},
			},
		},
		Stats: domain.PackageStats{FromPrice: 950000, FromCurrency: "SAR"},
	}, nil, domain.BrochureBrand{NameEN: "Al Noor Travel", NameAR: "النور", Logo: buf.Bytes(), LogoType: "image/png"}, domain.BrochureEN)
	raw, err := NewRenderer().Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Fatalf("not a pdf: %q", raw[:min(20, len(raw))])
	}
	if !bytes.Contains(raw, []byte("%%EOF")) || len(raw) < 4000 {
		t.Fatalf("pdf looks empty (%d bytes)", len(raw))
	}
}

func TestRendererArabic(t *testing.T) {
	doc := domain.BuildBrochure(domain.Package{
		Code: "CUS-2026-LUX-01", NameEN: "Cappadocia", NameAR: "كابادوكيا",
		Header: domain.Header{Kind: domain.KindOther, Category: "luxury", DurationDays: 5, TransportMode: "road", BaseCurrency: "USD"},
		Spec: domain.Spec{
			Destination: domain.Destination{Country: "Turkey", Districts: []string{"Istanbul"}},
			Services:    []domain.Activity{{Title: "نقل المطار", Details: "سيارة خاصة"}},
		},
	}, nil, domain.BrochureBrand{NameEN: "Wodi", NameAR: "ودي"}, domain.BrochureAR)
	raw, err := NewRenderer().Render(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Fatal("arabic brochure must still be a pdf")
	}
}

func TestShapeArabicKeepsLatin(t *testing.T) {
	got := shapeArabic("عمرة IST-JED")
	if !strings.Contains(got, "IST-JED") {
		t.Fatalf("latin run dropped: %q", got)
	}
	if strings.Contains(got, "عمرة") && !strings.ContainsFunc(got, func(r rune) bool { return r >= 0xFE70 && r <= 0xFEFF }) {
		t.Fatalf("arabic was not shaped: %q", got)
	}
}
