package tourpackage

import (
	"fmt"
	"strings"
)

func brochureFeatures(p Package, cp brochureCopy) []BrochureFeature {
	var out []BrochureFeature
	add := func(icon, title, detail, tone string) {
		title = strings.TrimSpace(title)
		if title == "" {
			return
		}
		out = append(out, BrochureFeature{Icon: icon, Title: title, Detail: strings.TrimSpace(detail), Tone: tone})
	}

	if p.DurationDays > 0 {
		add("calendar", cp.Duration, cp.days(p.DurationDays), "orange")
	}
	if p.TransportMode != "" {
		add("plane", cp.Transport, cp.transport(p.TransportMode), "amber")
	}
	if p.CapacityTotal > 0 {
		add("users", cp.Group, fmt.Sprintf("%d", p.CapacityTotal), "slate")
	}

	if IsPilgrimage(p.Kind) {
		if n := p.Spec.Nights.Madinah; n > 0 || p.Spec.Madinah.Name != "" {
			add("mosque", cp.city("madinah"), stayDetail(p.Spec.Madinah, n, cp), "emerald")
		}
		if n := p.Spec.Nights.Makkah; n > 0 || p.Spec.Makkah.Name != "" {
			add("kaaba", cp.city("makkah"), stayDetail(p.Spec.Makkah, n, cp), "rose")
		}
	} else {
		if country := strings.TrimSpace(p.Spec.Destination.Country); country != "" {
			place := country
			if d := strings.Join(p.Spec.Destination.Districts, " · "); d != "" {
				place += " · " + d
			}
			add("pin", cp.Destination, place, "teal")
		}
		for _, s := range p.Spec.Stays {
			if s.City == "" && s.Hotel.Name == "" {
				continue
			}
			title := orHotel(s.City, cp.Stay)
			add("hotel", title, stayDetail(s.Hotel, s.Nights, cp), "teal")
		}
	}

	f := p.Spec.Flights
	if f.Airline != "" || f.Outbound.Route != "" {
		add("plane", orHotel(f.Airline, cp.Flights), essence(f.Outbound.Route, cp.routing(f.Routing)), "amber")
	}
	if f.Inbound.Route != "" {
		add("return", cp.ChReturn, f.Inbound.Route, "slate")
	}
	if p.Spec.Transfers.Intercity != "" {
		add("bus", cp.Ground, cp.intercity(p.Spec.Transfers.Intercity), "sky")
	}
	if p.Spec.Bus.Company != "" {
		add("bus", p.Spec.Bus.Company, p.Spec.Bus.Details, "sky")
	}
	if p.Spec.Transfers.AirportMeet {
		add("plane", cp.AirportMeet, "", "amber")
	}
	if p.Spec.Transfers.HotelTransfers {
		add("bus", cp.hotelTransfers, "", "sky")
	}

	if p.Spec.Guidance.LeaderName != "" {
		add("guide", cp.Leader, p.Spec.Guidance.LeaderName, "violet")
	}
	if IsPilgrimage(p.Kind) && p.Spec.Guidance.FemaleGuide {
		add("users", strings.TrimSuffix(cp.femaleLine, "."), "", "violet")
	}

	if IsPilgrimage(p.Kind) {
		if p.Spec.Visa.Type != "" {
			add("stamp", cp.Visa, cp.visa(p.Spec.Visa.Type), "violet")
		}
		if p.Spec.Visa.HealthInsurance {
			add("check", cp.Insurance, "", "sky")
		}
		for _, k := range p.Spec.Kit {
			add(kitIcon(k), cp.kit(k), "", "orange")
		}
		for _, z := range p.Spec.Ziyarat.Madinah {
			add("star", cp.ziyarat(z), cp.city("madinah"), "emerald")
		}
		for _, z := range p.Spec.Ziyarat.Makkah {
			add("star", cp.ziyarat(z), cp.city("makkah"), "rose")
		}
		for _, line := range p.Spec.Included {
			add("check", line, "", "emerald")
		}
	} else {
		for _, a := range p.Spec.Services {
			if a.Title == "" {
				continue
			}
			add("check", a.Title, a.Details, "emerald")
		}
		for _, a := range p.Spec.Activities {
			if a.Title == "" {
				continue
			}
			add("star", a.Title, a.Details, "orange")
		}
	}
	return out
}

func stayDetail(h Hotel, nights int, cp brochureCopy) string {
	var bits []string
	if h.Name != "" {
		bits = append(bits, h.Name)
	}
	if nights > 0 {
		bits = append(bits, cp.nights(nights))
	}
	if h.Stars > 0 {
		bits = append(bits, fmt.Sprintf("%d-star", h.Stars))
	}
	if b := cp.board(h.Board); b != "" {
		bits = append(bits, b)
	}
	if a := cp.access(h.Access); a != "" {
		bits = append(bits, a)
	}
	if h.DistanceM > 0 {
		bits = append(bits, fmt.Sprintf(cp.metresFrom, h.DistanceM))
	}
	return essence(bits...)
}

func kitIcon(key string) string {
	switch key {
	case "luggage":
		return "luggage"
	case "zamzam":
		return "moon"
	case "prayer_book":
		return "stamp"
	default:
		return "check"
	}
}

func essence(parts ...string) string {
	out := parts[:0]
	for _, s := range parts {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, "  ·  ")
}
