package tourpackage

import (
	"strings"
)

func (c brochureCopy) join(items []string) string {
	clean := items[:0]
	for _, s := range items {
		if s = strings.TrimSpace(s); s != "" {
			clean = append(clean, s)
		}
	}
	switch len(clean) {
	case 0:
		return ""
	case 1:
		return clean[0]
	case 2:
		return clean[0] + c.And + clean[1]
	default:
		return strings.Join(clean[:len(clean)-1], c.Comma) + c.And + clean[len(clean)-1]
	}
}

func narrateDepart(p Package, cp brochureCopy) string {
	if p.TransportMode == "road" && strings.TrimSpace(p.Spec.Flights.Airline) == "" {
		return essence(p.Spec.Bus.Company, p.Spec.Bus.Details, cp.transport(p.TransportMode))
	}
	return essence(p.Spec.Flights.Airline, p.Spec.Flights.Outbound.Route, cp.routing(p.Spec.Flights.Routing))
}

func narrateStay(nights int, hotel Hotel, cityKey, _ string, _ string, ziyarat []string, _ []ItineraryDay, _ Package, cp brochureCopy) string {
	z := ziyaratNames(ziyarat, cp)
	return essence(stayDetail(hotel, nights, cp), cp.join(z))
}

func narrateTransfer(p Package, cp brochureCopy) string {
	mode := cp.intercity(p.Spec.Transfers.Intercity)
	if mode == "" {
		mode = cp.Coach
	}
	return essence(mode, p.Spec.Bus.Company)
}

func narrateReturn(p Package, cp brochureCopy) string {
	return essence(p.Spec.Flights.Inbound.Route, p.Spec.Flights.Airline)
}

func narrateArrive(p Package, place string, cp brochureCopy) string {
	return essence(place, cp.transport(p.TransportMode))
}

func narrateCity(s Stay, place string, _ []ItineraryDay, cp brochureCopy) string {
	city := orHotel(s.City, place)
	return essence(city, stayDetail(s.Hotel, s.Nights, cp))
}

func ziyaratNames(keys []string, cp brochureCopy) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if s := cp.ziyarat(k); s != "" {
			out = append(out, s)
		}
	}
	return out
}
