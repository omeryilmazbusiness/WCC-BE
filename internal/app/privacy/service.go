// Package privacy serves passport reveals (T-261) and KVKK data subject
// requests (T-267). Every disclosure or erasure is audited; when the audit
// write fails nothing is disclosed.
package privacy

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/privacy"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/ratelimit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// CustomerReader loads a customer within the caller's scope; (nil, nil)
// when it is missing or out of scope.
type CustomerReader interface {
	FindByID(ctx context.Context, id uuid.UUID) (*customer.Customer, error)
}

// BookingReader loads bookings and participants within the caller's scope.
type BookingReader interface {
	FindByID(ctx context.Context, id uuid.UUID) (*booking.Booking, error)
	ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]booking.Participant, error)
}

// Actor identifies who performs a request, for the audit trail.
type Actor struct {
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

type AnonymizeResult struct {
	CustomerID   uuid.UUID `json:"customer_id"`
	Placeholder  string    `json:"full_name"`
	AnonymizedAt time.Time `json:"anonymized_at"`
}

type Service struct {
	customers CustomerReader
	bookings  BookingReader
	store     domain.Store
	tx        tx.Runner
	audit     audit.Recorder
	limiter   ratelimit.Window
	limit     int
	window    time.Duration
	now       func() time.Time
}

func NewService(customers CustomerReader, bookings BookingReader, store domain.Store, txm tx.Runner, rec audit.Recorder) *Service {
	return &Service{
		customers: customers, bookings: bookings, store: store, tx: txm, audit: rec,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// SetRevealLimit caps passport reveals per user and window.
func (s *Service) SetRevealLimit(l ratelimit.Window, limit int, window time.Duration) {
	s.limiter, s.limit, s.window = l, limit, window
}

func (s *Service) checkRevealLimit(ctx context.Context, actor Actor) error {
	if s.limiter == nil || s.limit <= 0 {
		return nil
	}
	n, err := s.limiter.Hit(ctx, "pii_reveal:"+actor.UserID.String(), s.window)
	if err != nil {
		return nil // the limiter degrades open; reveals stay permission-gated and audited
	}
	if n > s.limit {
		return shared.NewRateLimited("too many passport reveals; try again later", s.window)
	}
	return nil
}

func (s *Service) findCustomer(ctx context.Context, id uuid.UUID) (*customer.Customer, error) {
	c, err := s.customers.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, shared.NewNotFound("customer")
	}
	return c, nil
}

func (s *Service) record(ctx context.Context, actor Actor, in audit.RecordInput) error {
	in.ActorID, in.IP, in.UserAgent = actor.UserID, actor.IP, actor.UserAgent
	return s.audit.Record(ctx, in)
}

// RevealCustomerPassport returns the customer's full passport number.
func (s *Service) RevealCustomerPassport(ctx context.Context, actor Actor, customerID uuid.UUID) (string, error) {
	if err := s.checkRevealLimit(ctx, actor); err != nil {
		return "", err
	}
	c, err := s.findCustomer(ctx, customerID)
	if err != nil {
		return "", err
	}
	if err := s.record(ctx, actor, audit.RecordInput{
		Action: "pii.revealed", EntityType: "customer", EntityID: &c.ID, BranchID: &c.BranchID,
		Extra: map[string]any{"field": "passport"},
	}); err != nil {
		return "", err
	}
	return c.PassportNo, nil
}

// RevealParticipantPassport returns a booking participant's full passport number.
func (s *Service) RevealParticipantPassport(ctx context.Context, actor Actor, bookingID, participantID uuid.UUID) (string, error) {
	if err := s.checkRevealLimit(ctx, actor); err != nil {
		return "", err
	}
	b, err := s.bookings.FindByID(ctx, bookingID)
	if err != nil || b == nil {
		return "", shared.NewNotFound("booking")
	}
	participants, err := s.bookings.ListParticipants(ctx, bookingID)
	if err != nil {
		return "", err
	}
	for _, p := range participants {
		if p.ID != participantID {
			continue
		}
		if err := s.record(ctx, actor, audit.RecordInput{
			Action: "pii.revealed", EntityType: "booking_participant", EntityID: &p.ID, BranchID: &b.BranchID,
			Extra: map[string]any{"field": "passport", "booking_id": b.ID},
		}); err != nil {
			return "", err
		}
		return p.PassportNo, nil
	}
	return "", shared.NewNotFound("participant")
}

// Export assembles the customer's personal data bundle.
func (s *Service) Export(ctx context.Context, actor Actor, customerID uuid.UUID) (*domain.Bundle, error) {
	c, err := s.findCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	out := &domain.Bundle{GeneratedAt: s.now(), Customer: customerData(c)}
	if out.Bookings, err = s.store.Bookings(ctx, c.ID); err != nil {
		return nil, err
	}
	if out.Payments, err = s.store.PaymentTotals(ctx, c.ID); err != nil {
		return nil, err
	}
	if out.Documents, err = s.store.Documents(ctx, c.ID); err != nil {
		return nil, err
	}
	if out.Conversations, err = s.store.Conversations(ctx, c.ID); err != nil {
		return nil, err
	}
	if out.Companions, err = s.store.Companions(ctx, c.ID); err != nil {
		return nil, err
	}
	nonNil(out)
	if err := s.record(ctx, actor, audit.RecordInput{
		Action: "privacy.exported", EntityType: "customer", EntityID: &c.ID, BranchID: &c.BranchID,
		Extra: map[string]any{
			"bookings": len(out.Bookings), "documents": len(out.Documents), "conversations": len(out.Conversations),
		},
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// Anonymize irreversibly erases the customer's personal data. Customers
// with bookings that are not completed or cancelled are refused.
func (s *Service) Anonymize(ctx context.Context, actor Actor, customerID uuid.UUID, reason string) (*AnonymizeResult, error) {
	reason, err := domain.ValidateReason(reason)
	if err != nil {
		return nil, err
	}
	var out *AnonymizeResult
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		c, err := s.findCustomer(ctx, customerID)
		if err != nil {
			return err
		}
		if c.AnonymizedAt != nil {
			return &shared.AppError{Code: "customer_already_anonymized", Message: "customer is already anonymized", Err: shared.ErrConflict}
		}
		active, err := s.store.HasActiveBookings(ctx, c.ID)
		if err != nil {
			return err
		}
		if active {
			return domain.ErrActiveBookings()
		}
		at := s.now()
		placeholder := domain.Placeholder(c.ID)
		if err := s.store.Anonymize(ctx, domain.Anonymization{CustomerID: c.ID, Placeholder: placeholder, At: at}); err != nil {
			return err
		}
		if err := s.record(ctx, actor, audit.RecordInput{
			Action: "privacy.anonymized", EntityType: "customer", EntityID: &c.ID, BranchID: &c.BranchID,
			After: map[string]any{"full_name": placeholder, "anonymized_at": at},
			Extra: map[string]any{"reason": reason},
		}); err != nil {
			return err
		}
		out = &AnonymizeResult{CustomerID: c.ID, Placeholder: placeholder, AnonymizedAt: at}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func customerData(c *customer.Customer) domain.CustomerData {
	d := domain.CustomerData{
		ID: c.ID, BranchID: c.BranchID, FullName: c.FullName, FullNameAR: c.FullNameAR,
		Phone: c.Phone, Email: c.Email, Nationality: c.Nationality, PassportNo: c.PassportNo,
		Preferences: c.Preferences, SpecialRequirements: c.SpecialRequirements, Notes: c.Notes,
		IsActive: c.IsActive, MergedIntoID: c.MergedIntoID, AnonymizedAt: c.AnonymizedAt,
		CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if c.DateOfBirth != nil {
		dob := c.DateOfBirth.Format("2006-01-02")
		d.DateOfBirth = &dob
	}
	if len(d.Preferences) == 0 {
		d.Preferences = []byte(`{}`)
	}
	return d
}

func nonNil(b *domain.Bundle) {
	if b.Bookings == nil {
		b.Bookings = []domain.Booking{}
	}
	for i := range b.Bookings {
		if b.Bookings[i].Participants == nil {
			b.Bookings[i].Participants = []domain.Participant{}
		}
	}
	if b.Payments == nil {
		b.Payments = []domain.PaymentsTotal{}
	}
	if b.Documents == nil {
		b.Documents = []domain.Document{}
	}
	if b.Conversations == nil {
		b.Conversations = []domain.Conversation{}
	}
	if b.Companions == nil {
		b.Companions = []domain.CompanionRecord{}
	}
}
