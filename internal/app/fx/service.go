package fx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Options carries optional collaborators. Without Providers the sync job is
// a no-op; Location is the business time zone for "today" (UTC when nil).
type Options struct {
	Providers []domain.RateProvider
	Location  *time.Location
	Log       *slog.Logger
}

// Service manages FX rates (T-272). Every mutation is audited in the same
// transaction (fail closed).
type Service struct {
	repo      domain.Repository
	tx        tx.Runner
	audit     audit.Recorder
	converter domain.Converter
	providers []domain.RateProvider
	loc       *time.Location
	log       *slog.Logger
	now       func() time.Time
}

func NewService(repo domain.Repository, txm tx.Runner, auditor audit.Recorder, opts Options) *Service {
	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		repo: repo, tx: txm, audit: auditor, converter: NewConverter(repo),
		providers: opts.Providers, loc: loc, log: log, now: time.Now,
	}
}

// Converter exposes the rate-backed converter for other modules.
func (s *Service) Converter() domain.Converter { return s.converter }

// Today is the current calendar date in the business time zone.
func (s *Service) Today() time.Time { return domain.DateOf(s.now().In(s.loc)) }

type CreateInput struct {
	Base          string
	Quote         string
	Rate          string
	EffectiveDate string
	Source        string
	ActorID       uuid.UUID
}

type UpdateInput struct {
	ID      uuid.UUID
	Rate    string
	Source  *string
	ActorID uuid.UUID
}

// ConvertResult is a conversion ready for the API.
type ConvertResult struct {
	Amount    int64
	From      string
	To        string
	Converted int64
	Rate      domain.Rate
}

func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.StoredRate, int64, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, 0, err
	}
	var err error
	if f.Base != "" {
		if f.Base, err = domain.NormalizeCurrency(f.Base); err != nil {
			return nil, 0, err
		}
	}
	if f.Quote != "" {
		if f.Quote, err = domain.NormalizeCurrency(f.Quote); err != nil {
			return nil, 0, err
		}
	}
	return s.repo.List(ctx, f)
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.StoredRate, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, err
	}
	base, err := domain.NormalizeCurrency(in.Base)
	if err != nil {
		return nil, err
	}
	quote, err := domain.NormalizeCurrency(in.Quote)
	if err != nil {
		return nil, err
	}
	if base == quote {
		return nil, shared.NewValidation("base and quote must differ")
	}
	scaled, err := domain.ParseRate(in.Rate)
	if err != nil {
		return nil, err
	}
	day, err := domain.ParseDate(in.EffectiveDate)
	if err != nil {
		return nil, err
	}
	rate := &domain.StoredRate{
		ID: uuid.New(),
		Rate: domain.Rate{
			Base: base, Quote: quote, Scaled: scaled, EffectiveDate: day, Source: sourceOr(in.Source, "manual"),
		},
		CreatedAt: s.now().UTC(),
	}
	if in.ActorID != uuid.Nil {
		actor := in.ActorID
		rate.CreatedBy = &actor
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Insert(ctx, rate); err != nil {
			return mapInsertErr(err)
		}
		return s.record(ctx, in.ActorID, "fx.rate_created", rate.ID, nil, snapshot(rate), nil)
	})
	if err != nil {
		return nil, err
	}
	return rate, nil
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*domain.StoredRate, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, err
	}
	scaled, err := domain.ParseRate(in.Rate)
	if err != nil {
		return nil, err
	}
	var out *domain.StoredRate
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		rate, err := s.repo.Get(ctx, in.ID)
		if err != nil {
			return err
		}
		before := snapshot(rate)
		rate.Scaled = scaled
		if in.Source != nil {
			rate.Source = sourceOr(*in.Source, "manual")
		}
		if err := s.repo.UpdateRate(ctx, rate.ID, rate.Scaled, rate.Source); err != nil {
			return err
		}
		out = rate
		return s.record(ctx, in.ActorID, "fx.rate_updated", rate.ID, before, snapshot(rate), nil)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) Delete(ctx context.Context, id, actorID uuid.UUID) error {
	if _, err := access.Require(ctx); err != nil {
		return err
	}
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		rate, err := s.repo.Get(ctx, id)
		if err != nil {
			return err
		}
		if err := s.repo.Delete(ctx, id); err != nil {
			return err
		}
		return s.record(ctx, actorID, "fx.rate_deleted", id, snapshot(rate), nil, nil)
	})
}

// Convert converts amount on the given date (today when nil).
func (s *Service) Convert(ctx context.Context, amount int64, from, to string, on *time.Time) (*ConvertResult, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, err
	}
	day := s.Today()
	if on != nil {
		day = domain.DateOf(*on)
	}
	c, err := s.converter.Convert(ctx, amount, from, to, day)
	switch {
	case errors.Is(err, domain.ErrRateNotFound):
		return nil, &shared.AppError{Code: "fx_rate_not_found", Message: "no effective rate for this pair and date", Err: shared.ErrNotFound}
	case errors.Is(err, domain.ErrOverflow):
		return nil, shared.NewValidation("amount is too large to convert")
	case err != nil:
		return nil, err
	}
	return &ConvertResult{Amount: amount, From: c.Rate.Base, To: c.Rate.Quote, Converted: c.Amount, Rate: c.Rate}, nil
}

// SyncRates stores today's rates from every provider without overwriting
// existing ones. A failing provider does not block the others; their errors
// are returned joined. It is a no-op without providers.
func (s *Service) SyncRates(ctx context.Context) (int, error) {
	day := s.Today()
	inserted := 0
	var errs []error
	for _, p := range s.providers {
		n, err := s.syncProvider(ctx, p, day)
		inserted += n
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.Name(), err))
		}
	}
	return inserted, errors.Join(errs...)
}

func (s *Service) syncProvider(ctx context.Context, p domain.RateProvider, day time.Time) (int, error) {
	rates, err := p.Fetch(ctx, day)
	if err != nil {
		return 0, err
	}
	inserted := 0
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		inserted = 0
		for _, r := range rates {
			base, errB := domain.NormalizeCurrency(r.Base)
			quote, errQ := domain.NormalizeCurrency(r.Quote)
			if errB != nil || errQ != nil || base == quote || r.Scaled <= 0 {
				s.log.Warn("fx provider returned an invalid rate", "provider", p.Name(), "base", r.Base, "quote", r.Quote)
				continue
			}
			eff := day
			if !r.EffectiveDate.IsZero() {
				eff = domain.DateOf(r.EffectiveDate)
			}
			stored := &domain.StoredRate{
				ID:        uuid.New(),
				Rate:      domain.Rate{Base: base, Quote: quote, Scaled: r.Scaled, EffectiveDate: eff, Source: sourceOr(r.Source, p.Name())},
				CreatedAt: s.now().UTC(),
			}
			ok, err := s.repo.InsertIfAbsent(ctx, stored)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			inserted++
			if err := s.record(ctx, uuid.Nil, "fx.rate_created", stored.ID, nil, snapshot(stored),
				map[string]any{"provider": p.Name()}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}

func (s *Service) record(ctx context.Context, actor uuid.UUID, action string, id uuid.UUID, before, after any, extra map[string]any) error {
	return recordRate(ctx, s.audit, actor, action, id, before, after, extra)
}

func recordRate(ctx context.Context, rec audit.Recorder, actor uuid.UUID, action string, id uuid.UUID, before, after any, extra map[string]any) error {
	if rec == nil {
		return nil
	}
	return rec.Record(ctx, audit.RecordInput{
		ActorID: actor, Action: action, EntityType: "fx_rate", EntityID: &id,
		Before: before, After: after, Extra: extra,
	})
}

func mapInsertErr(err error) error {
	if errors.Is(err, domain.ErrRateExists) {
		return &shared.AppError{
			Code: "fx_rate_exists", Message: "a rate for this pair and effective date already exists",
			Err: shared.ErrConflict,
		}
	}
	return err
}

func snapshot(r *domain.StoredRate) map[string]any {
	return map[string]any{
		"base": r.Base, "quote": r.Quote, "rate": domain.FormatRate(r.Scaled),
		"effective_date": r.EffectiveDate.Format(time.DateOnly), "source": r.Source,
	}
}

func sourceOr(s, fallback string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return fallback
}
