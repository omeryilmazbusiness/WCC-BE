package planlimit

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/company"
)

// Store loads the tenant plan and how many of a limited resource it already uses.
type Store interface {
	GetCompany(ctx context.Context, id uuid.UUID) (*domain.Company, error)
	CountUsage(ctx context.Context, companyID uuid.UUID, kind domain.LimitKind) (int, error)
}

// Guard refuses writes that would exceed the caller's company plan.
type Guard struct {
	store Store
}

func New(store Store) *Guard {
	return &Guard{store: store}
}

// Refuse is a no-op for platform operators (they create the first GM) and
// when the store is unset (unit tests that do not care about plans).
func (g *Guard) Refuse(ctx context.Context, kind domain.LimitKind) error {
	if g == nil || g.store == nil {
		return nil
	}
	sc := access.From(ctx)
	if sc.Level == access.LevelGlobal || sc.CompanyID == uuid.Nil {
		return nil
	}
	c, err := g.store.GetCompany(ctx, sc.CompanyID)
	if err != nil {
		return err
	}
	used, err := g.store.CountUsage(ctx, sc.CompanyID, kind)
	if err != nil {
		return err
	}
	return domain.Refuse(c.Plan, kind, used)
}

// DailyAI is the model-answer cap for the caller's plan (0 = unlimited).
func (g *Guard) DailyAI(ctx context.Context) int {
	if g == nil || g.store == nil {
		return 0
	}
	sc := access.From(ctx)
	if sc.CompanyID == uuid.Nil {
		return 0
	}
	c, err := g.store.GetCompany(ctx, sc.CompanyID)
	if err != nil {
		return 0
	}
	return domain.Of(c.Plan).Limits.AIPerDay
}
