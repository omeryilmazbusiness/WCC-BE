package tourpackage_test

import (
	"strings"
	"testing"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

func header() domain.Header {
	return domain.Header{Kind: "umrah", Category: "ramadan_last15", DurationDays: 14, TransportMode: "flight_scheduled", BaseCurrency: "SAR"}
}

func validSpec() domain.Spec {
	return domain.Spec{
		Nights:  domain.Nights{Makkah: 10, Madinah: 4},
		Makkah:  domain.Hotel{Name: " Swissôtel Makkah ", Stars: 5, DistanceM: 50, Access: "WALKING", Board: "BB", CheckIn: "2026-03-01", CheckOut: "2026-03-11"},
		Madinah: domain.Hotel{Name: "Pullman Zamzam", Stars: 5, DistanceM: 150, Access: "walking", Zone: "markaziyya_north", Board: "fb", CheckIn: "2026-03-11", CheckOut: "2026-03-15"},
		Flights: domain.Flights{Airline: "THY", Routing: "direct", PNR: "abc123",
			Outbound: domain.FlightLeg{Route: "ist-jed", FlightNo: "tk92", Date: "2026-03-01"},
			Inbound:  domain.FlightLeg{Route: "MED-IST", Date: "2026-03-15"}},
		Transfers:    domain.Transfers{Intercity: "haramain_train", AirportMeet: true},
		Visa:         domain.Visa{Type: "umrah_visa", HealthInsurance: true},
		Kit:          []string{"zamzam", "IHRAM", "ihram"},
		Ziyarat:      domain.Ziyarat{Makkah: []string{"hira", "arafat"}, Madinah: []string{"quba"}},
		Itinerary:    []domain.ItineraryDay{{Day: 2, City: "makkah", Title: "Ziyarat"}, {Day: 1, City: "Makkah", Title: "Arrival & first Umrah"}, {Day: 5}},
		Requirements: domain.DefaultRequirements(),
		Costs:        domain.Costs{Flight: 300000, Hotel: 500000, Visa: 50000, MarkupPct: 20},
	}
}

func TestNormalizeSpecCanonicalises(t *testing.T) {
	s := validSpec()
	if err := domain.NormalizeSpec(&s, header()); err != nil {
		t.Fatal(err)
	}
	if s.Makkah.Name != "Swissôtel Makkah" || s.Makkah.Access != "walking" || s.Makkah.Board != "bb" {
		t.Fatalf("hotel not normalised: %+v", s.Makkah)
	}
	if s.Flights.Outbound.Route != "IST-JED" || s.Flights.Outbound.FlightNo != "TK92" || s.Flights.PNR != "ABC123" {
		t.Fatalf("flights not normalised: %+v", s.Flights)
	}
	if s.Visa.Type != "UMRAH_VISA" {
		t.Fatalf("visa=%s", s.Visa.Type)
	}
	if strings.Join(s.Kit, ",") != "ihram,zamzam" {
		t.Fatalf("kit=%v (deduped, canonical order)", s.Kit)
	}
	if strings.Join(s.Ziyarat.Makkah, ",") != "arafat,hira" {
		t.Fatalf("ziyarat=%v", s.Ziyarat.Makkah)
	}
	if len(s.Itinerary) != 2 || s.Itinerary[0].Day != 1 || s.Itinerary[0].City != "makkah" {
		t.Fatalf("itinerary should drop empty days and sort: %+v", s.Itinerary)
	}
	if s.Costs.Currency != "SAR" {
		t.Fatalf("costs currency defaults to base, got %s", s.Costs.Currency)
	}
}

func TestNormalizeSpecRejects(t *testing.T) {
	cases := map[string]func(*domain.Spec){
		"nights over duration":  func(s *domain.Spec) { s.Nights.Makkah = 12 },
		"stay mismatch":         func(s *domain.Spec) { s.Makkah.CheckOut = "2026-03-12" },
		"checkout before in":    func(s *domain.Spec) { s.Madinah.CheckOut = "2026-03-10"; s.Nights.Madinah = 0 },
		"bad board":             func(s *domain.Spec) { s.Makkah.Board = "ai" },
		"bad stars":             func(s *domain.Spec) { s.Madinah.Stars = 6 },
		"bad route":             func(s *domain.Spec) { s.Flights.Outbound.Route = "ISTANBUL" },
		"inbound before out":    func(s *domain.Spec) { s.Flights.Inbound.Date = "2026-02-20" },
		"bad visa":              func(s *domain.Spec) { s.Visa.Type = "WORK" },
		"short kit":             func(s *domain.Spec) { s.Kit = []string{"x"} },
		"itinerary past end":    func(s *domain.Spec) { s.Itinerary = []domain.ItineraryDay{{Day: 15, Title: "x"}} },
		"itinerary duplicate":   func(s *domain.Spec) { s.Itinerary = []domain.ItineraryDay{{Day: 1, Title: "a"}, {Day: 1, Title: "b"}} },
		"itinerary no title":    func(s *domain.Spec) { s.Itinerary = []domain.ItineraryDay{{Day: 3, Details: "free day"}} },
		"negative cost":         func(s *domain.Spec) { s.Costs.Hotel = -1 },
		"markup too high":       func(s *domain.Spec) { s.Costs.MarkupPct = 400 },
		"passport months range": func(s *domain.Spec) { s.Requirements.PassportMonths = 30 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			s := validSpec()
			mutate(&s)
			if err := domain.NormalizeSpec(&s, header()); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNormalizeHeader(t *testing.T) {
	h := domain.Header{Kind: " HAJJ ", Category: "", BaseCurrency: ""}
	if err := domain.NormalizeHeader(&h); err != nil {
		t.Fatal(err)
	}
	if h.Kind != "hajj" || h.Category != "standard" || h.BaseCurrency != "SAR" || h.TransportMode != "flight_scheduled" {
		t.Fatalf("defaults not applied: %+v", h)
	}
	bad := []domain.Header{
		{Kind: "tour"},
		{Kind: "umrah", Category: "short"},
		{Kind: "hajj", Category: "ramadan_full"},
		{Kind: "umrah", TransportMode: "ship"},
		{Kind: "umrah", BaseCurrency: "XYZ"},
		{Kind: "umrah", DurationDays: 61},
		{Kind: "umrah", CapacityTotal: -1},
	}
	for _, b := range bad {
		if err := domain.NormalizeHeader(&b); err == nil {
			t.Fatalf("expected error for %+v", b)
		}
	}
}

func TestCostsSuggestedPrice(t *testing.T) {
	c := domain.Costs{Flight: 300000, Hotel: 500000, Visa: 50000, Transfer: 20000, Guidance: 10000, Gifts: 5050, MarkupPct: 20}
	if c.Total() != 885050 {
		t.Fatalf("total=%d", c.Total())
	}
	// 885050 * 1.2 = 1062060 → rounded to whole SAR = 1062100
	if got := c.SuggestedPrice(); got != 1062100 {
		t.Fatalf("suggested=%d", got)
	}
}

func TestPassportValidFor(t *testing.T) {
	r := domain.DefaultRequirements()
	depart := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	if !r.PassportValidFor(time.Date(2027, 2, 28, 0, 0, 0, 0, time.UTC), depart) {
		t.Fatal("six months from Aug 31 is Feb 28")
	}
	if r.PassportValidFor(time.Date(2027, 2, 27, 0, 0, 0, 0, time.UTC), depart) {
		t.Fatal("one day short must fail")
	}
}

func TestNormalizeHeaderOtherAndTiers(t *testing.T) {
	h := domain.Header{Kind: "OTHER", Category: "luxury"}
	if err := domain.NormalizeHeader(&h); err != nil {
		t.Fatal(err)
	}
	if h.Kind != domain.KindOther || h.Category != "luxury" {
		t.Fatalf("got %+v", h)
	}
	legacy := domain.Header{Kind: "umrah", Category: "ramadan_last15"}
	if err := domain.NormalizeHeader(&legacy); err != nil {
		t.Fatal(err)
	}
	hajjEco := domain.Header{Kind: "hajj", Category: "economy"}
	if err := domain.NormalizeHeader(&hajjEco); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeSpecOtherCustomTour(t *testing.T) {
	s := domain.Spec{
		Destination: domain.Destination{Country: "  Turkey ", Districts: []string{"Istanbul", "istanbul", "Cappadocia", ""}},
		Stays: []domain.Stay{
			{City: " Istanbul ", Nights: 3, Hotel: domain.Hotel{Name: " Pera Palace ", Stars: 5, Board: "BB", CheckIn: "2026-05-01", CheckOut: "2026-05-04"}},
			{City: "Cappadocia", Nights: 2, Hotel: domain.Hotel{Name: "Cave Suites", Stars: 4}},
		},
		Bus:        domain.Bus{Company: " Metro Turizm ", Seats: 45, Price: 150000, Details: " VIP 2+1 "},
		Transfers:  domain.Transfers{Intercity: "PRIVATE_CAR", AirportMeet: true},
		Visa:       domain.Visa{Type: "UMRAH_VISA", HealthInsurance: true},
		Kit:        []string{"ihram", "zamzam"},
		Ziyarat:    domain.Ziyarat{Makkah: []string{"hira"}},
		Included:   []string{"Umrah visa"},
		Services:   []domain.Activity{{Title: " Airport transfer "}, {Title: "City card", Details: " 48h "}},
		Activities: []domain.Activity{{Title: " Bosphorus cruise "}, {Title: "", Details: ""}},
		Itinerary:  []domain.ItineraryDay{{Day: 1, City: "Istanbul", Title: "Arrival"}},
	}
	h := domain.Header{Kind: domain.KindOther, Category: "standard", DurationDays: 7, TransportMode: "flight_scheduled", BaseCurrency: "USD"}
	if err := domain.NormalizeSpec(&s, h); err != nil {
		t.Fatal(err)
	}
	if s.Destination.Country != "Turkey" || len(s.Destination.Districts) != 2 {
		t.Fatalf("destination=%+v", s.Destination)
	}
	if s.Stays[0].Hotel.Name != "Pera Palace" || s.Stays[0].Hotel.Board != "bb" {
		t.Fatalf("stay=%+v", s.Stays[0])
	}
	if s.Bus.Company != "Metro Turizm" || s.Transfers.Intercity != "private_car" {
		t.Fatalf("bus/ground %+v %+v", s.Bus, s.Transfers)
	}
	if s.Visa.Type != "" || s.Visa.HealthInsurance || len(s.Kit) != 0 || len(s.Ziyarat.Makkah) != 0 || len(s.Included) != 0 || s.Guidance.FemaleGuide {
		t.Fatalf("other must drop pilgrimage offer fields: visa=%+v kit=%v ziyarat=%+v included=%v", s.Visa, s.Kit, s.Ziyarat, s.Included)
	}
	if len(s.Services) != 2 || s.Services[0].Title != "Airport transfer" || s.Services[1].Details != "48h" {
		t.Fatalf("services=%v", s.Services)
	}
	if len(s.Activities) != 1 || s.Activities[0].Title != "Bosphorus cruise" {
		t.Fatalf("activities=%v", s.Activities)
	}
}

func TestNormalizeSpecOtherRequiresDestination(t *testing.T) {
	s := domain.Spec{}
	h := domain.Header{Kind: domain.KindOther, Category: "economy", DurationDays: 5, TransportMode: "road", BaseCurrency: "TRY"}
	if err := domain.NormalizeSpec(&s, h); err == nil {
		t.Fatal("expected destination error")
	}
	s.Destination = domain.Destination{Country: "Georgia", Districts: []string{"Tbilisi"}}
	if err := domain.NormalizeSpec(&s, h); err != nil {
		t.Fatal(err)
	}
	s.Services = []domain.Activity{{Details: "included"}}
	if err := domain.NormalizeSpec(&s, h); err == nil {
		t.Fatal("expected untitled service error")
	}
	s.Services = nil
	s.Activities = []domain.Activity{{Details: "free walking"}}
	if err := domain.NormalizeSpec(&s, h); err == nil {
		t.Fatal("expected untitled activity error")
	}
}

func TestPackageRemaining(t *testing.T) {
	p := domain.Package{Header: domain.Header{CapacityTotal: 45}, Stats: domain.PackageStats{Reserved: 30, DepartureSeats: 80}}
	if p.Remaining() != 15 {
		t.Fatalf("quota remaining=%d", p.Remaining())
	}
	p.CapacityTotal = 0
	if p.Remaining() != 50 {
		t.Fatalf("departure remaining=%d", p.Remaining())
	}
}
