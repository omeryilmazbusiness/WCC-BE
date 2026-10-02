package flight

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// AviasalesLinker builds Aviasales links carrying the platform's affiliate
// marker, so every booking is credited to the platform, not a tenant.
type AviasalesLinker struct {
	BaseURL string
	Marker  string
}

// searchPath is "/search/IST1012DXB" + passenger code (+ optional "DDMM" return).
var searchPath = regexp.MustCompile(`^/search/([A-Z]{3}\d{4}[A-Z]{3})(\d+)$`)

// BookingURL opens the provider's search for this fare, re-coded for the travellers.
func (l AviasalesLinker) BookingURL(f Fare, p Passengers) string {
	link := f.Link
	if link == "" {
		link = "/search/" + f.Origin + f.LocalDeparture().Format("0201") + f.Destination + "1"
	}
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	u.Path = withPassengers(u.Path, p)
	return l.absolute(u)
}

// SearchURL opens the provider's search for the whole query; it is the way
// out when no fares can be shown here.
func (l AviasalesLinker) SearchURL(q Query) string {
	u := &url.URL{Path: "/search/" + q.Origin + q.Departure.Format("0201") + q.Destination + passengerCode(q.Passengers)}
	return l.absolute(u)
}

func (l AviasalesLinker) absolute(u *url.URL) string {
	base, err := url.Parse(strings.TrimRight(l.BaseURL, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return ""
	}
	v := u.Query()
	if l.Marker != "" {
		v.Set("marker", l.Marker)
	}
	out := *base
	out.Path = base.Path + u.Path
	out.RawQuery = v.Encode()
	return out.String()
}

func withPassengers(path string, p Passengers) string {
	route, rest, ok := splitSearchPath(path)
	if !ok {
		return path
	}
	return "/search/" + route + passengerCode(p) + rest
}

// splitSearchPath separates the route from the trailing digits: the first is
// the old passenger code, a 4-digit tail after it is a return date.
func splitSearchPath(path string) (route, rest string, ok bool) {
	m := searchPath.FindStringSubmatch(path)
	if m == nil {
		return "", "", false
	}
	digits := m[2]
	if len(digits) > 4 {
		rest = digits[len(digits)-4:]
	}
	return m[1], rest, true
}

// passengerCode is adults, then children and infants only when needed ("2", "21", "201").
func passengerCode(p Passengers) string {
	code := strconv.Itoa(p.Adults)
	if p.Children > 0 || p.Infants > 0 {
		code += strconv.Itoa(p.Children)
	}
	if p.Infants > 0 {
		code += strconv.Itoa(p.Infants)
	}
	return code
}
