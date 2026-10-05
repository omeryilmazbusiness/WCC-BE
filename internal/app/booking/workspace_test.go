package booking_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type memCollab struct {
	books   *memBookingRepo
	notes   map[uuid.UUID]*domain.Note
	changes map[uuid.UUID]*domain.ChangeRequest
}

var _ domain.CollabStore = (*memCollab)(nil)

func newMemCollab(books *memBookingRepo) *memCollab {
	return &memCollab{books: books, notes: map[uuid.UUID]*domain.Note{}, changes: map[uuid.UUID]*domain.ChangeRequest{}}
}

func (m *memCollab) UpdateProfile(_ context.Context, b *domain.Booking) error {
	cur := m.books.byID[b.ID]
	cur.PNR, cur.ServiceType, cur.SupplierSource = b.PNR, b.ServiceType, b.SupplierSource
	cur.Channel, cur.Summary, cur.CompanyName = b.Channel, b.Summary, b.CompanyName
	return nil
}
func (m *memCollab) SaveHold(_ context.Context, b *domain.Booking) error {
	m.books.byID[b.ID].HoldExpiresAt = b.HoldExpiresAt
	return nil
}
func (m *memCollab) IncrementReissue(_ context.Context, id uuid.UUID) error {
	m.books.byID[id].ReissueCount++
	return nil
}
func (m *memCollab) CreateNote(_ context.Context, n *domain.Note) error {
	cp := *n
	m.notes[n.ID] = &cp
	return nil
}
func (m *memCollab) FindNote(_ context.Context, bookingID, noteID uuid.UUID) (*domain.Note, error) {
	n, ok := m.notes[noteID]
	if !ok || n.BookingID != bookingID {
		return nil, shared.NewNotFound("note")
	}
	cp := *n
	return &cp, nil
}
func (m *memCollab) DeleteNote(_ context.Context, _, noteID uuid.UUID) error {
	delete(m.notes, noteID)
	return nil
}
func (m *memCollab) ListNotes(_ context.Context, bookingID uuid.UUID) ([]domain.Note, error) {
	var out []domain.Note
	for _, n := range m.notes {
		if n.BookingID == bookingID {
			out = append(out, *n)
		}
	}
	return out, nil
}
func (m *memCollab) CreateChange(_ context.Context, c *domain.ChangeRequest) error {
	cp := *c
	m.changes[c.ID] = &cp
	return nil
}
func (m *memCollab) FindChangeForUpdate(_ context.Context, bookingID, changeID uuid.UUID) (*domain.ChangeRequest, error) {
	c, ok := m.changes[changeID]
	if !ok || c.BookingID != bookingID {
		return nil, shared.NewNotFound("change request")
	}
	cp := *c
	return &cp, nil
}
func (m *memCollab) ResolveChange(_ context.Context, c *domain.ChangeRequest) error {
	cp := *c
	m.changes[c.ID] = &cp
	return nil
}
func (m *memCollab) ListChanges(_ context.Context, bookingID uuid.UUID) ([]domain.ChangeRequest, error) {
	var out []domain.ChangeRequest
	for _, c := range m.changes {
		if c.BookingID == bookingID {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (m *memCollab) Stats(context.Context, domain.ListFilter) (domain.Stats, error) {
	return domain.Stats{}, nil
}
func (m *memCollab) ListActivity(context.Context, uuid.UUID, int) ([]domain.Activity, error) {
	return nil, nil
}

func newWorkspaceFixture() (*fixture, *memCollab) {
	f := newFixture(10)
	c := newMemCollab(f.books)
	f.svc.SetCollabStore(c)
	return f, c
}

func TestWorkspaceUnconfiguredIsUnavailable(t *testing.T) {
	f := newFixture(10)
	b := f.draft(t, 1, 1000)
	if _, err := f.svc.ListNotes(sysCtx(), b.ID); err == nil {
		t.Fatal("expected an error without a collab store")
	}
}

func TestUpdateProfileNormalizesAndValidates(t *testing.T) {
	f, _ := newWorkspaceFixture()
	b := f.draft(t, 1, 1000)

	got, err := f.svc.UpdateProfile(sysCtx(), b.ID, domain.Profile{
		PNR: " abc123 ", ServiceType: domain.ServiceFlight, SupplierSource: "duffel", Channel: "b2b_agency", Summary: "IST - JFK",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.PNR != "ABC123" || got.ServiceType != domain.ServiceFlight || got.Channel != "b2b_agency" {
		t.Fatalf("profile not applied: %+v", got.Profile())
	}

	if _, err := f.svc.UpdateProfile(sysCtx(), b.ID, domain.Profile{PNR: "bad pnr!"}); err == nil {
		t.Fatalf("invalid PNR accepted: %v", err)
	}
	if f.books.byID[b.ID].PNR != "ABC123" {
		t.Fatal("invalid update must not overwrite the stored profile")
	}
}

func TestNotesOnlyAuthorDeletes(t *testing.T) {
	f, c := newWorkspaceFixture()
	b := f.draft(t, 1, 1000)
	author, other := uuid.New(), uuid.New()

	if _, err := f.svc.AddNote(sysCtx(), b.ID, author, "   ", false); err == nil {
		t.Fatal("blank note accepted")
	}
	n, err := f.svc.AddNote(sysCtx(), b.ID, author, "  call back after visa  ", true)
	if err != nil {
		t.Fatal(err)
	}
	if n.Body != "call back after visa" || !n.Pinned {
		t.Fatalf("note not normalized: %+v", n)
	}
	if err := f.svc.DeleteNote(sysCtx(), b.ID, n.ID, other); err == nil {
		t.Fatal("non-author deleted a note")
	}
	if err := f.svc.DeleteNote(sysCtx(), b.ID, n.ID, author); err != nil {
		t.Fatal(err)
	}
	if len(c.notes) != 0 {
		t.Fatal("note not deleted")
	}
}

func TestChangeRequestLifecycle(t *testing.T) {
	f, _ := newWorkspaceFixture()
	b := f.draft(t, 1, 1000)
	actor := uuid.New()

	if _, err := f.svc.RequestChange(sysCtx(), b.ID, actor, "date_change", ""); err == nil {
		t.Fatal("change without details accepted")
	}
	cr, err := f.svc.RequestChange(sysCtx(), b.ID, actor, "date_change", "move to 12 May")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ResolveChange(sysCtx(), b.ID, cr.ID, actor, domain.ChangeRejected, ""); err == nil {
		t.Fatal("rejection without a note accepted")
	}
	done, err := f.svc.ResolveChange(sysCtx(), b.ID, cr.ID, actor, domain.ChangeCompleted, "")
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != domain.ChangeCompleted || f.books.byID[b.ID].ReissueCount != 1 {
		t.Fatalf("completion did not mark reissue: %+v count=%d", done, f.books.byID[b.ID].ReissueCount)
	}
	if _, err := f.svc.ResolveChange(sysCtx(), b.ID, cr.ID, actor, domain.ChangeCompleted, ""); err == nil {
		t.Fatal("resolved twice")
	}
}

func TestPaymentLink(t *testing.T) {
	f, _ := newWorkspaceFixture()
	b := f.draft(t, 1, 1000)

	_, err := f.svc.CreatePaymentLink(sysCtx(), b.ID, 0)
	if code(err) != appbooking.CodePaymentGatewayUnconfigured {
		t.Fatalf("want unconfigured error, got %v", err)
	}

	f.svc.SetPaymentLinkTemplate("https://pay.example/checkout?ref={ref}&amount={amount}&cur={currency}")
	link, err := f.svc.CreatePaymentLink(sysCtx(), b.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if link.Amount != f.books.byID[b.ID].BalanceAmt || !strings.Contains(link.URL, "amount=1000") || !strings.Contains(link.URL, "cur=USD") {
		t.Fatalf("unexpected link: %+v", link)
	}
	if _, err := f.svc.CreatePaymentLink(sysCtx(), b.ID, 5000); err == nil {
		t.Fatal("amount above balance accepted")
	}
}

func TestRecordShareValidates(t *testing.T) {
	f, _ := newWorkspaceFixture()
	b := f.draft(t, 1, 1000)
	if err := f.svc.RecordShare(sysCtx(), b.ID, "fax", "voucher"); err == nil {
		t.Fatal("unknown channel accepted")
	}
	if err := f.svc.RecordShare(sysCtx(), b.ID, "whatsapp", "voucher"); err != nil {
		t.Fatal(err)
	}
}
