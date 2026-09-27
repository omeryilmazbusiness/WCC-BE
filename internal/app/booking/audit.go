package booking

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
)

// record writes an audit event; called inside the unit of work so a failed
// insert rolls the change back (fail closed). The actor comes from ctx.
func (s *Service) record(ctx context.Context, action, entityType string, entityID, branchID uuid.UUID, before, after any, extra map[string]any) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, audit.RecordInput{
		Action: action, EntityType: entityType, EntityID: &entityID, BranchID: &branchID,
		Before: before, After: after, Extra: extra,
	})
}

func (s *Service) recordBooking(ctx context.Context, action string, b *domain.Booking, before, after any, extra map[string]any) error {
	return s.record(ctx, action, "booking", b.ID, b.BranchID, before, after, extra)
}

func (s *Service) recordParticipant(ctx context.Context, action string, b *domain.Booking, participantID uuid.UUID, before, after *domain.Participant) error {
	return s.record(ctx, action, "booking_participant", participantID, b.BranchID,
		participantSnapshot(before), participantSnapshot(after), map[string]any{"booking_id": b.ID})
}

func bookingSnapshot(b *domain.Booking) map[string]any {
	return map[string]any{
		"status": b.Status, "pax_count": b.PaxCount, "total_amount": b.TotalAmount,
		"discount_amt": b.DiscountAmt, "tax_amt": b.TaxAmt, "fee_amt": b.FeeAmt, "cost_amt": b.CostAmt, "collected_amt": b.CollectedAmt,
		"balance_amt": b.BalanceAmt, "currency": b.Currency, "notes": b.Notes, "owner_id": b.OwnerID,
		"departure_id": b.DepartureID, "customer_id": b.CustomerID,
	}
}

// participantSnapshot never includes passport data; only whether one is on file.
func participantSnapshot(p *domain.Participant) map[string]any {
	if p == nil {
		return nil
	}
	m := map[string]any{
		"full_name": p.FullName, "nationality": p.Nationality, "passport_on_file": !p.PassportMissing(),
	}
	if p.DateOfBirth != nil {
		m["date_of_birth"] = p.DateOfBirth.Format("2006-01-02")
	}
	return m
}

func lineItemsSnapshot(items []domain.LineItem) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		it := items[i]
		out = append(out, map[string]any{
			"kind": it.Kind, "category": it.Category, "label": it.Label, "quantity": it.Quantity,
			"unit_price": it.UnitPrice, "unit_cost": it.UnitCost,
		})
	}
	return out
}

func totalsSnapshot(b *domain.Booking) map[string]any {
	return map[string]any{
		"subtotal": b.Subtotal(), "discount_amt": b.DiscountAmt, "tax_amt": b.TaxAmt, "fee_amt": b.FeeAmt,
		"total_amount": b.TotalAmount, "cost_amt": b.CostAmt, "balance_amt": b.BalanceAmt,
	}
}

// recordTransition audits a lifecycle transition as booking.status_changed,
// or booking.status_overridden with the bypassed guards. soldBefore/soldAfter
// < 0 mean departure capacity was not touched.
func (s *Service) recordTransition(ctx context.Context, b *domain.Booking, res domain.TransitionResult, soldBefore, soldAfter int, more map[string]any) error {
	extra := map[string]any{"departure_id": b.DepartureID, "pax_count": b.PaxCount, "actor_kind": res.Actor}
	for k, v := range more {
		extra[k] = v
	}
	if soldBefore >= 0 || soldAfter >= 0 {
		extra["capacity_sold_before"] = soldBefore
		extra["capacity_sold_after"] = soldAfter
	}
	action := "booking.status_changed"
	if res.Actor == domain.ActorOverride {
		action = "booking.status_overridden"
		bypassed := make([]string, len(res.Bypassed))
		for i, g := range res.Bypassed {
			bypassed[i] = string(g)
		}
		extra["guards_bypassed"] = bypassed
	}
	return s.recordBooking(ctx, action, b,
		map[string]any{"status": res.From, "hold_expires_at": res.PrevHold},
		map[string]any{"status": res.To, "hold_expires_at": b.HoldExpiresAt, "status_reason": b.StatusReason},
		extra)
}
