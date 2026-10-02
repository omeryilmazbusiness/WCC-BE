package travelpayouts

import (
	"context"
	"net/url"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/flight"
)

// Places reads the public autocomplete (`GET /places2`, no token needed).
type Places struct {
	cfg Config
	t   transport
}

func NewPlaces(cfg Config) *Places {
	cfg = cfg.withDefaults()
	return &Places{cfg: cfg, t: newTransport(cfg)}
}

type placeRow struct {
	Type        string `json:"type"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	CityCode    string `json:"city_code"`
	CityName    string `json:"city_name"`
	CountryCode string `json:"country_code"`
	CountryName string `json:"country_name"`
}

func (p *Places) Places(ctx context.Context, term, locale string) ([]domain.Place, error) {
	v := url.Values{}
	v.Set("term", term)
	v.Set("locale", locale)
	v.Add("types[]", "city")
	v.Add("types[]", "airport")
	var rows []placeRow
	if err := p.t.getJSON(ctx, p.cfg.AutocompleteURL+"/places2?"+v.Encode(), false, &rows); err != nil {
		return nil, err
	}
	out := make([]domain.Place, 0, len(rows))
	for _, r := range rows {
		if r.Code == "" || (r.Type != "city" && r.Type != "airport") {
			continue
		}
		place := domain.Place{
			Code: r.Code, Type: r.Type, Name: r.Name,
			CityCode: r.CityCode, CityName: r.CityName,
			CountryCode: r.CountryCode, CountryName: r.CountryName,
		}
		if place.Type == "city" {
			place.CityCode, place.CityName = place.Code, place.Name
		}
		out = append(out, place)
	}
	return out, nil
}
