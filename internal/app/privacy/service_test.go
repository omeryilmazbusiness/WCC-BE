package privacy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	appprivacy "github.com/wodi-crm/wodi-crm-be/internal/app/privacy"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/privacy"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/ratelimit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type customers map[uuid.UUID]*customer.Customer

func (c customers) FindByID(_ context.Context, id uuid.UUID) (*customer.Customer, error) {
	return c[id], nil
}

type bookings struct {
	b     *booking.Booking
	parts []booking.Participant
}

func (f bookings) FindByID(_ context.Context, id uuid.UUID) (*booking.Booking, error) {
	if f.b == nil || f.b.ID != id {
		return nil, errors.New("no rows in result set")
	}
	return f.b, nil
}

func (f bookings) ListParticipants(context.Context, uuid.UUID) ([]booking.Participant, error) {
	return f.parts, nil
}

type store struct {
	active     bool
	anonymized []domain.Anonymization
}

func (s *store) Bookings(context.Context, uuid.UUID) ([]domain.Booking, error) {
	return []domain.Booking{{ID: uuid.New()}}, nil
}
func (s *store) PaymentTotals(context.Context, uuid.UUID) ([]domain.PaymentsTotal, error) {
	return nil, nil
}
func (s *store) Documents(context.Context, uuid.UUID) ([]domain.Document, error) { return nil, nil }
func (s *store) Conversations(context.Context, uuid.UUID) ([]domain.Conversation, error) {
	return nil, nil
}
func (s *store) Companions(context.Context, uuid.UUID) ([]domain.CompanionRecord, error) {
	return nil, nil
}
func (s *store) HasActiveBookings(context.Context, uuid.UUID) (bool, error) { return s.active, nil }
func (s *store) Anonymize(_ context.Context, a domain.Anonymization) error {
	s.anonymized = append(s.anonymized, a)
	return nil
}

type recorder struct {
	events []audit.RecordInput
	err    error
}

func (r *recorder) Record(_ context.Context, in audit.RecordInput) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, in)
	return nil
}

var actor = appprivacy.Actor{UserID: uuid.New(), IP: "10.0.0.1", UserAgent: "test"}

func fixture() (*customer.Customer, customers) {
	c := &customer.Customer{ID: uuid.New(), BranchID: uuid.New(), FullName: "Ayşe Yılmaz", PassportNo: "U12345678"}
	return c, customers{c.ID: c}
}

func TestRevealCustomerPassportAudits(t *testing.T) {
	c, cs := fixture()
	rec := &recorder{}
	svc := appprivacy.NewService(cs, bookings{}, &store{}, tx.Nop{}, rec)

	got, err := svc.RevealCustomerPassport(context.Background(), actor, c.ID)
	if err != nil || got != "U12345678" {
		t.Fatalf("reveal = %q, %v", got, err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("audit events = %d", len(rec.events))
	}
	ev := rec.events[0]
	if ev.Action != "pii.revealed" || ev.EntityType != "customer" || *ev.EntityID != c.ID ||
		ev.ActorID != actor.UserID || ev.IP != actor.IP || ev.UserAgent != actor.UserAgent ||
		ev.Extra["field"] != "passport" {
		t.Fatalf("audit = %+v", ev)
	}
}

func TestRevealOutOfScopeIsNotFound(t *testing.T) {
	svc := appprivacy.NewService(customers{}, bookings{}, &store{}, tx.Nop{}, &recorder{})
	_, err := svc.RevealCustomerPassport(context.Background(), actor, uuid.New())
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	_, err = svc.RevealParticipantPassport(context.Background(), actor, uuid.New(), uuid.New())
	if !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("participant err = %v", err)
	}
}

func TestRevealFailsClosedWhenAuditFails(t *testing.T) {
	c, cs := fixture()
	svc := appprivacy.NewService(cs, bookings{}, &store{}, tx.Nop{}, &recorder{err: errors.New("db down")})
	if got, err := svc.RevealCustomerPassport(context.Background(), actor, c.ID); err == nil || got != "" {
		t.Fatalf("reveal without audit = %q, %v", got, err)
	}
}

func TestRevealParticipantPassport(t *testing.T) {
	b := &booking.Booking{ID: uuid.New(), BranchID: uuid.New()}
	p := booking.Participant{ID: uuid.New(), BookingID: b.ID, PassportNo: "P7654321"}
	rec := &recorder{}
	svc := appprivacy.NewService(customers{}, bookings{b: b, parts: []booking.Participant{p}}, &store{}, tx.Nop{}, rec)

	got, err := svc.RevealParticipantPassport(context.Background(), actor, b.ID, p.ID)
	if err != nil || got != "P7654321" {
		t.Fatalf("reveal = %q, %v", got, err)
	}
	if rec.events[0].EntityType != "booking_participant" || *rec.events[0].EntityID != p.ID {
		t.Fatalf("audit = %+v", rec.events[0])
	}
	if _, err := svc.RevealParticipantPassport(context.Background(), actor, b.ID, uuid.New()); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("unknown participant err = %v", err)
	}
}

func TestRevealRateLimit(t *testing.T) {
	c, cs := fixture()
	svc := appprivacy.NewService(cs, bookings{}, &store{}, tx.Nop{}, &recorder{})
	svc.SetRevealLimit(ratelimit.NewMemory(), 2, time.Minute)
	for i := 0; i < 2; i++ {
		if _, err := svc.RevealCustomerPassport(context.Background(), actor, c.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.RevealCustomerPassport(context.Background(), actor, c.ID); !errors.Is(err, shared.ErrRateLimited) {
		t.Fatalf("third reveal err = %v", err)
	}
}

func TestExportAuditsAndHasNoNilCollections(t *testing.T) {
	c, cs := fixture()
	rec := &recorder{}
	svc := appprivacy.NewService(cs, bookings{}, &store{}, tx.Nop{}, rec)
	b, err := svc.Export(context.Background(), actor, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Customer.PassportNo != "U12345678" || b.Documents == nil || b.Payments == nil ||
		b.Conversations == nil || b.Companions == nil || b.Bookings[0].Participants == nil {
		t.Fatalf("bundle = %+v", b)
	}
	if rec.events[0].Action != "privacy.exported" {
		t.Fatalf("audit = %+v", rec.events)
	}
}

func TestAnonymize(t *testing.T) {
	c, cs := fixture()
	st := &store{}
	rec := &recorder{}
	svc := appprivacy.NewService(cs, bookings{}, st, tx.Nop{}, rec)

	if _, err := svc.Anonymize(context.Background(), actor, c.ID, "too short"); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("short reason err = %v", err)
	}
	res, err := svc.Anonymize(context.Background(), actor, c.ID, "KVKK erasure request #42")
	if err != nil {
		t.Fatal(err)
	}
	if len(st.anonymized) != 1 || res.Placeholder != domain.Placeholder(c.ID) || res.Placeholder == "" {
		t.Fatalf("result = %+v, store = %+v", res, st.anonymized)
	}
	ev := rec.events[0]
	if ev.Action != "privacy.anonymized" || ev.Extra["reason"] != "KVKK erasure request #42" {
		t.Fatalf("audit = %+v", ev)
	}

	now := time.Now()
	c.AnonymizedAt = &now
	if _, err := svc.Anonymize(context.Background(), actor, c.ID, "KVKK erasure request #42"); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("second anonymize err = %v", err)
	}
}

func TestAnonymizeRefusesActiveBookings(t *testing.T) {
	c, cs := fixture()
	st := &store{active: true}
	svc := appprivacy.NewService(cs, bookings{}, st, tx.Nop{}, &recorder{})
	_, err := svc.Anonymize(context.Background(), actor, c.ID, "KVKK erasure request #42")
	var appErr *shared.AppError
	if !errors.As(err, &appErr) || appErr.Code != "customer_has_active_bookings" || !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	if len(st.anonymized) != 0 {
		t.Fatal("customer with active bookings was anonymized")
	}
}
