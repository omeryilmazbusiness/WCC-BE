package tourpackage

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
)

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

// recordDeparture runs inside the caller's transaction so capacity changes
// fail closed when the audit insert fails.
func (s *Service) recordDeparture(ctx context.Context, action string, d *domain.Departure, before, after map[string]any) error {
	if s.audit == nil {
		return nil
	}
	var branch *uuid.UUID
	if p, err := s.repo.FindPackage(ctx, d.PackageID); err == nil && p != nil {
		b := p.BranchID
		branch = &b
	}
	id := d.ID
	return s.audit.Record(ctx, audit.RecordInput{
		Action: action, EntityType: "departure", EntityID: &id, BranchID: branch,
		Before: before, After: after,
		Extra: map[string]any{"package_id": d.PackageID, "code": d.Code},
	})
}

func departureSnapshot(d *domain.Departure) map[string]any {
	return map[string]any{
		"code": d.Code, "depart_date": d.DepartDate.Format("2006-01-02"), "return_date": d.ReturnDate.Format("2006-01-02"),
		"capacity_total": d.CapacityTotal, "capacity_sold": d.CapacitySold, "base_price": d.BasePrice,
		"currency": d.Currency, "is_active": d.IsActive, "sales_closed": d.SalesClosed,
		"soft_threshold_pct": d.SoftThresholdPct, "allow_oversell": d.AllowOversell,
	}
}

func capacityChanged(a, b *domain.Departure) bool {
	return a.CapacityTotal != b.CapacityTotal || a.AllowOversell != b.AllowOversell ||
		a.SoftThresholdPct != b.SoftThresholdPct || a.SalesClosed != b.SalesClosed
}
