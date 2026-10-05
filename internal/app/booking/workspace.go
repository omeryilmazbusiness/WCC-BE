package booking

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// SetCollabStore wires notes, change requests, profile and activity storage.
func (s *Service) SetCollabStore(c domain.CollabStore) { s.collab = c }

// SetPaymentLinkTemplate configures the provider checkout URL template.
func (s *Service) SetPaymentLinkTemplate(t string) { s.payLink = strings.TrimSpace(t) }

func (s *Service) collabStore() (domain.CollabStore, error) {
	if s.collab == nil {
		return nil, shared.NewUnavailable("booking workspace is not configured", 0)
	}
	return s.collab, nil
}

// Stats counts the operations segments over the list filter.
func (s *Service) Stats(ctx context.Context, in ListInput) (domain.Stats, error) {
	c, err := s.collabStore()
	if err != nil {
		return domain.Stats{}, err
	}
	f, err := s.listFilter(ctx, in)
	if err != nil {
		return domain.Stats{}, err
	}
	return c.Stats(ctx, f)
}

// UpdateProfile replaces PNR, service type, supplier source, channel,
// summary and company name.
func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, p domain.Profile) (*domain.Booking, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		before := b.Profile()
		changed, err := b.ApplyProfile(p, s.now())
		if err != nil || !changed {
			return err
		}
		if err := c.UpdateProfile(ctx, b); err != nil {
			return err
		}
		return s.recordBooking(ctx, "booking.profile_updated", b, profileSnapshot(before), profileSnapshot(b.Profile()), nil)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// ExtendHold moves the option deadline later.
func (s *Service) ExtendHold(ctx context.Context, id uuid.UUID, until time.Time) (*domain.Booking, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.store.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		prev, err := b.ExtendHold(until, s.now())
		if err != nil {
			return err
		}
		if err := c.SaveHold(ctx, b); err != nil {
			return err
		}
		return s.recordBooking(ctx, "booking.hold_extended", b,
			map[string]any{"hold_expires_at": prev}, map[string]any{"hold_expires_at": b.HoldExpiresAt}, nil)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// CancellationQuote prices a cancellation today with the default policy.
func (s *Service) CancellationQuote(ctx context.Context, id uuid.UUID) (domain.CancellationQuote, error) {
	b, err := s.Get(ctx, id)
	if err != nil {
		return domain.CancellationQuote{}, err
	}
	dep, err := s.departures.FindDeparture(ctx, b.DepartureID)
	if err != nil {
		return domain.CancellationQuote{}, shared.NewNotFound("departure")
	}
	days := domain.DaysUntil(dep.DepartDate, s.now().In(s.loc))
	return b.QuoteCancellation(days, domain.DefaultCancellationPolicy()), nil
}

func (s *Service) ListNotes(ctx context.Context, bookingID uuid.UUID) ([]domain.Note, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	return c.ListNotes(ctx, bookingID)
}

func (s *Service) AddNote(ctx context.Context, bookingID, actor uuid.UUID, body string, pinned bool) (*domain.Note, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	n, err := domain.NewNote(bookingID, actor, body, pinned, s.now())
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.Get(ctx, bookingID)
		if err != nil {
			return err
		}
		if err := c.CreateNote(ctx, n); err != nil {
			return err
		}
		return s.record(ctx, "booking.note_added", "booking_note", n.ID, b.BranchID, nil, nil,
			map[string]any{"booking_id": b.ID, "pinned": n.Pinned})
	})
	if err != nil {
		return nil, err
	}
	return c.FindNote(ctx, bookingID, n.ID)
}

func (s *Service) DeleteNote(ctx context.Context, bookingID, noteID, actor uuid.UUID) error {
	c, err := s.collabStore()
	if err != nil {
		return err
	}
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.Get(ctx, bookingID)
		if err != nil {
			return err
		}
		n, err := c.FindNote(ctx, bookingID, noteID)
		if err != nil {
			return err
		}
		if !n.CanDelete(actor) {
			return shared.NewForbidden("only the author can delete a note")
		}
		if err := c.DeleteNote(ctx, bookingID, noteID); err != nil {
			return err
		}
		return s.record(ctx, "booking.note_deleted", "booking_note", n.ID, b.BranchID, nil, nil,
			map[string]any{"booking_id": b.ID})
	})
}

func (s *Service) ListChanges(ctx context.Context, bookingID uuid.UUID) ([]domain.ChangeRequest, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	return c.ListChanges(ctx, bookingID)
}

func (s *Service) RequestChange(ctx context.Context, bookingID, actor uuid.UUID, kind, details string) (*domain.ChangeRequest, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	var out *domain.ChangeRequest
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.Get(ctx, bookingID)
		if err != nil {
			return err
		}
		cr, err := domain.NewChangeRequest(b, kind, details, actor, s.now())
		if err != nil {
			return err
		}
		if err := c.CreateChange(ctx, cr); err != nil {
			return err
		}
		out = cr
		return s.record(ctx, "booking.change_requested", "booking_change", cr.ID, b.BranchID, nil, nil,
			map[string]any{"booking_id": b.ID, "kind": cr.Kind})
	})
	return out, err
}

// ResolveChange completes or rejects a change request; completion marks the
// booking reissued.
func (s *Service) ResolveChange(ctx context.Context, bookingID, changeID, actor uuid.UUID, status, note string) (*domain.ChangeRequest, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	var out *domain.ChangeRequest
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		b, err := s.Get(ctx, bookingID)
		if err != nil {
			return err
		}
		cr, err := c.FindChangeForUpdate(ctx, bookingID, changeID)
		if err != nil {
			return err
		}
		if err := cr.Resolve(status, note, actor, s.now()); err != nil {
			return err
		}
		if err := c.ResolveChange(ctx, cr); err != nil {
			return err
		}
		if cr.Status == domain.ChangeCompleted {
			if err := c.IncrementReissue(ctx, bookingID); err != nil {
				return err
			}
		}
		out = cr
		return s.record(ctx, "booking.change_"+cr.Status, "booking_change", cr.ID, b.BranchID, nil, nil,
			map[string]any{"booking_id": b.ID, "kind": cr.Kind, "note": cr.ResolutionNote})
	})
	return out, err
}

func (s *Service) Activity(ctx context.Context, bookingID uuid.UUID, limit int) ([]domain.Activity, error) {
	c, err := s.collabStore()
	if err != nil {
		return nil, err
	}
	return c.ListActivity(ctx, bookingID, limit)
}

// Share channels and documents recorded in the activity trail.
var (
	shareChannels  = []string{"whatsapp", "email", "sms", "copy"}
	shareDocuments = []string{"voucher", "eticket", "proforma", "contract", "receipt", "payment_link", "summary"}
)

// RecordShare audits that a document was sent to the customer; delivery
// happens on the agent's device (WhatsApp / mail client).
func (s *Service) RecordShare(ctx context.Context, bookingID uuid.UUID, channel, document string) error {
	if !oneOfStr(channel, shareChannels) {
		return shared.NewValidation("invalid channel")
	}
	if !oneOfStr(document, shareDocuments) {
		return shared.NewValidation("invalid document")
	}
	b, err := s.Get(ctx, bookingID)
	if err != nil {
		return err
	}
	return s.recordBooking(ctx, "booking.shared", b, nil, nil, map[string]any{"channel": channel, "document": document})
}

// PaymentLink is a provider checkout URL for part or all of the balance.
type PaymentLink struct {
	URL      string `json:"url"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Ref      string `json:"ref"`
}

const CodePaymentGatewayUnconfigured = "payment_gateway_unconfigured"

// CreatePaymentLink renders the configured checkout URL for amount (0 means
// the full balance).
func (s *Service) CreatePaymentLink(ctx context.Context, bookingID uuid.UUID, amount int64) (*PaymentLink, error) {
	if s.payLink == "" {
		return nil, &shared.AppError{Code: CodePaymentGatewayUnconfigured, Message: "payment gateway is not configured", Err: shared.ErrConflict}
	}
	b, err := s.Get(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.Status == domain.StatusCancelled || b.BalanceAmt <= 0 {
		return nil, shared.NewInvalidState("booking has no outstanding balance")
	}
	if amount == 0 {
		amount = b.BalanceAmt
	}
	if amount < 0 || amount > b.BalanceAmt {
		return nil, shared.NewValidation("amount must be between 0 and the outstanding balance")
	}
	ref := domain.RefCode(b.RefNo)
	link := &PaymentLink{URL: renderPayLink(s.payLink, ref, amount, b.Currency, b.ID), Amount: amount, Currency: b.Currency, Ref: ref}
	if err := s.recordBooking(ctx, "booking.payment_link_created", b, nil, nil,
		map[string]any{"amount": amount, "currency": b.Currency}); err != nil {
		return nil, err
	}
	return link, nil
}

func renderPayLink(tpl, ref string, amount int64, currency string, id uuid.UUID) string {
	return strings.NewReplacer(
		"{ref}", url.QueryEscape(ref),
		"{amount}", strconv.FormatInt(amount, 10),
		"{currency}", url.QueryEscape(currency),
		"{booking_id}", id.String(),
	).Replace(tpl)
}
