package booking

import (
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Lifecycle jobs (T-269).
const (
	JobHoldExpiry     shared.JobName = "booking.hold_expiry"
	JobTravelledSweep shared.JobName = "booking.travelled_sweep"
	JobRecompute      shared.JobName = "booking.recompute"
	JobRecomputeSweep shared.JobName = "booking.recompute_sweep"
)

type RecomputePayload struct {
	BookingID uuid.UUID `json:"booking_id"`
}
