package fx

import (
	"context"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
)

// RateReader is the lookup the converter needs (ISP).
type RateReader interface {
	Latest(ctx context.Context, base, quote string, on time.Time) (*domain.Rate, error)
}

// Converter implements domain.Converter over stored rates: the latest rate
// effective on the date, falling back to the inverse pair.
type Converter struct {
	rates RateReader
}

func NewConverter(rates RateReader) *Converter {
	return &Converter{rates: rates}
}

var _ domain.Converter = (*Converter)(nil)

func (c *Converter) Convert(ctx context.Context, amount int64, from, to string, on time.Time) (domain.Conversion, error) {
	from, err := domain.NormalizeCurrency(from)
	if err != nil {
		return domain.Conversion{}, err
	}
	if to, err = domain.NormalizeCurrency(to); err != nil {
		return domain.Conversion{}, err
	}
	day := domain.DateOf(on)
	if from == to {
		return domain.Conversion{Amount: amount, Rate: domain.Identity(from, day)}, nil
	}
	direct, err := c.rates.Latest(ctx, from, to, day)
	if err != nil {
		return domain.Conversion{}, err
	}
	inverse, err := c.rates.Latest(ctx, to, from, day)
	if err != nil {
		return domain.Conversion{}, err
	}
	rate, err := domain.Pick(direct, inverse)
	if err != nil {
		return domain.Conversion{}, err
	}
	converted, err := domain.Apply(amount, rate.Scaled)
	if err != nil {
		return domain.Conversion{}, err
	}
	return domain.Conversion{Amount: converted, Rate: rate}, nil
}
