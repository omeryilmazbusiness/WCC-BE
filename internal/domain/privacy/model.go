// Package privacy models KVKK data subject requests: the personal data
// export of a customer and irreversible anonymization (T-267).
package privacy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Bundle is the customer's personal data export. Collections are ordered by
// creation time then id so repeated exports of unchanged data are identical
// apart from GeneratedAt.
type Bundle struct {
	GeneratedAt   time.Time         `json:"generated_at"`
	Customer      CustomerData      `json:"customer"`
	Bookings      []Booking         `json:"bookings"`
	Payments      []PaymentsTotal   `json:"payments_summary"`
	Documents     []Document        `json:"documents"`
	Conversations []Conversation    `json:"conversations"`
	Companions    []CompanionRecord `json:"companions"`
}

// CustomerData carries the subject's own data, including the full passport.
type CustomerData struct {
	ID                  uuid.UUID       `json:"id"`
	BranchID            uuid.UUID       `json:"branch_id"`
	FullName            string          `json:"full_name"`
	FullNameAR          string          `json:"full_name_ar"`
	Phone               string          `json:"phone"`
	Email               string          `json:"email"`
	Nationality         string          `json:"nationality"`
	PassportNo          string          `json:"passport_no"`
	DateOfBirth         *string         `json:"date_of_birth"`
	Preferences         json.RawMessage `json:"preferences"`
	SpecialRequirements string          `json:"special_requirements"`
	Notes               string          `json:"notes"`
	IsActive            bool            `json:"is_active"`
	MergedIntoID        *uuid.UUID      `json:"merged_into_id"`
	AnonymizedAt        *time.Time      `json:"anonymized_at"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type Booking struct {
	ID           uuid.UUID     `json:"id"`
	DepartureID  uuid.UUID     `json:"departure_id"`
	Status       string        `json:"status"`
	PaxCount     int           `json:"pax_count"`
	TotalAmount  int64         `json:"total_amount"`
	CollectedAmt int64         `json:"collected_amt"`
	BalanceAmt   int64         `json:"balance_amt"`
	Currency     string        `json:"currency"`
	CreatedAt    time.Time     `json:"created_at"`
	Participants []Participant `json:"participants"`
}

// Participant may be another person travelling on the subject's booking, so
// only the passport suffix is exported.
type Participant struct {
	ID            uuid.UUID `json:"id"`
	FullName      string    `json:"full_name"`
	Nationality   string    `json:"nationality"`
	DateOfBirth   *string   `json:"date_of_birth"`
	PassportLast4 string    `json:"passport_last4"`
}

type PaymentsTotal struct {
	Currency string `json:"currency"`
	Count    int    `json:"count"`
	Total    int64  `json:"total"`
}

type Document struct {
	ID          uuid.UUID `json:"id"`
	RelatedType string    `json:"related_type"`
	RelatedID   uuid.UUID `json:"related_id"`
	Kind        string    `json:"kind"`
	FileName    string    `json:"file_name"`
	ContentType string    `json:"content_type"`
	SizeBytes   int64     `json:"size_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

type Conversation struct {
	ID             uuid.UUID  `json:"id"`
	Channel        string     `json:"channel"`
	Status         string     `json:"status"`
	Subject        string     `json:"subject"`
	MessageCount   int        `json:"message_count"`
	LastInboundAt  *time.Time `json:"last_inbound_at"`
	LastOutboundAt *time.Time `json:"last_outbound_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

type CompanionRecord struct {
	CompanionID uuid.UUID `json:"companion_id"`
	FullName    string    `json:"full_name"`
	Relation    string    `json:"relation"`
}

// Store reads and erases a customer's related personal data. Callers have
// already checked the customer is within their scope.
type Store interface {
	Bookings(ctx context.Context, customerID uuid.UUID) ([]Booking, error)
	PaymentTotals(ctx context.Context, customerID uuid.UUID) ([]PaymentsTotal, error)
	Documents(ctx context.Context, customerID uuid.UUID) ([]Document, error)
	Conversations(ctx context.Context, customerID uuid.UUID) ([]Conversation, error)
	Companions(ctx context.Context, customerID uuid.UUID) ([]CompanionRecord, error)
	// HasActiveBookings reports bookings that are neither completed nor cancelled.
	HasActiveBookings(ctx context.Context, customerID uuid.UUID) (bool, error)
	Anonymize(ctx context.Context, req Anonymization) error
}

// Anonymization describes one irreversible erasure.
type Anonymization struct {
	CustomerID uuid.UUID
	// Placeholder replaces the names of the customer, their leads and the
	// participant rows representing them on their own bookings.
	Placeholder string
	At          time.Time
}

// Placeholder is the display name of an anonymized customer.
func Placeholder(id uuid.UUID) string {
	return "Anonymized " + strings.SplitN(id.String(), "-", 2)[0]
}

// MinReasonLength is the shortest accepted anonymization reason.
const MinReasonLength = 10

// ValidateReason requires a meaningful, recorded justification.
func ValidateReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < MinReasonLength {
		return "", shared.NewValidation(fmt.Sprintf("reason must be at least %d characters", MinReasonLength))
	}
	return reason, nil
}

// ErrActiveBookings refuses anonymizing a customer who still travels.
func ErrActiveBookings() *shared.AppError {
	return &shared.AppError{
		Code:    "customer_has_active_bookings",
		Message: "customer has bookings that are not completed or cancelled",
		Err:     shared.ErrConflict,
	}
}
