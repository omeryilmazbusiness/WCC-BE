package payment

import (
	"context"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
)

// record writes an audit event inside the caller's unit of work so a failed
// insert rolls the financial change back (fail closed). actor is only a
// fallback for callers without a request actor on ctx.
func (s *Service) record(ctx context.Context, actor uuid.UUID, action, entityType string, entityID, branchID uuid.UUID, before, after any, extra map[string]any) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, audit.RecordInput{
		ActorID: actor, Action: action, EntityType: entityType, EntityID: &entityID, BranchID: &branchID,
		Before: before, After: after, Extra: extra,
	})
}

// recordPayment audits a ledger entry; moneyBefore is the booking's money
// snapshot before recomputation (nil when the booking was not recomputed).
func (s *Service) recordPayment(ctx context.Context, actor uuid.UUID, action string, p *domain.Payment, b *bookingdomain.Booking, before any, moneyBefore, extra map[string]any) error {
	if extra == nil {
		extra = map[string]any{}
	}
	if moneyBefore != nil {
		extra["booking_before"] = moneyBefore
		extra["booking_after"] = bookingMoney(b)
	}
	return s.record(ctx, actor, action, "payment", p.ID, b.BranchID, before, paymentSnapshot(p), extra)
}

func paymentSnapshot(p *domain.Payment) map[string]any {
	m := map[string]any{
		"booking_id": p.BookingID, "amount": p.Amount, "currency": p.Currency, "method": p.Method,
		"reference": p.Reference, "event_type": p.EventType, "status": p.Status, "recorded_by": p.RecordedBy,
		"note": p.Note,
	}
	if p.ReversesPaymentID != nil {
		m["reverses_payment_id"] = *p.ReversesPaymentID
	}
	if p.ApprovedBy != nil {
		m["approved_by"] = *p.ApprovedBy
	}
	if p.ApprovedAt != nil {
		m["approved_at"] = *p.ApprovedAt
	}
	return m
}

func bookingMoney(b *bookingdomain.Booking) map[string]any {
	return map[string]any{
		"total_amount": b.TotalAmount, "collected_amt": b.CollectedAmt, "balance_amt": b.BalanceAmt, "currency": b.Currency,
	}
}

func scheduleSnapshot(sc *domain.Schedule) map[string]any {
	return map[string]any{
		"booking_id": sc.BookingID, "amount": sc.Amount, "currency": sc.Currency,
		"due_at": sc.DueAt, "label": sc.Label, "status": sc.Status,
	}
}
