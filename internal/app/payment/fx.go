package payment

import (
	"context"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
)

// SettingsFX reads reporting currency from finance_settings (T-120).
type SettingsFX struct {
	Repo domain.Repository
}

func (s SettingsFX) ReportingCurrency(ctx context.Context, branchID uuid.UUID) (string, error) {
	if s.Repo == nil {
		return "SAR", nil
	}
	return s.Repo.GetFinanceSettings(ctx, branchID)
}

func (s SettingsFX) Convert(ctx context.Context, branchID uuid.UUID, amount int64, fromCurrency string) (int64, string, error) {
	rep, err := s.ReportingCurrency(ctx, branchID)
	if err != nil {
		return amount, fromCurrency, err
	}
	// P1 hook: identity conversion; rates table can replace this later.
	_ = fromCurrency
	return amount, rep, nil
}
