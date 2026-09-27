package payment

import (
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// InitialChargeStatus decides a new charge's status. auto_verify skips the
// verification step, so it needs the approver permission (T-276); asking for
// it without is rejected rather than silently downgraded.
func InitialChargeStatus(autoVerify, mayApprove bool) (Status, error) {
	if !autoVerify {
		return StatusUnverified, nil
	}
	if !mayApprove {
		return "", &shared.AppError{
			Code: "forbidden_auto_verify", Message: "auto_verify requires payments.approve", Err: shared.ErrForbidden,
		}
	}
	return StatusVerified, nil
}

// CheckRefundApprover enforces segregation of duties: whoever requested a
// refund (recorded the refund entry) cannot approve it.
func CheckRefundApprover(refund *Payment, approver uuid.UUID) error {
	if refund.RecordedBy == approver {
		return &shared.AppError{
			Code: "sod_violation", Message: "refund requester cannot approve the same refund", Err: shared.ErrForbidden,
		}
	}
	return nil
}

// ResolveReceivedAt defaults a missing received date to today and rejects
// future dates. Both are calendar dates in the business time zone.
func ResolveReceivedAt(receivedAt *time.Time, today time.Time) (time.Time, error) {
	today = fx.DateOf(today)
	if receivedAt == nil || receivedAt.IsZero() {
		return today, nil
	}
	d := fx.DateOf(*receivedAt)
	if d.After(today) {
		return time.Time{}, shared.NewValidation("received_at cannot be in the future")
	}
	return d, nil
}
