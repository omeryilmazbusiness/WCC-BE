package finance

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	supplierdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// ReconService runs BSP reconciliation, refund settlement quotes and
// balance confirmation letters.
type ReconService struct {
	Base
	bsp       BSPStore
	letters   LetterStore
	bookings  BookingLookup
	agencies  AgencyStore
	suppliers SupplierAccounts
	tx        tx.Runner
}

func NewReconService(b Base, bsp BSPStore, letters LetterStore, bookings BookingLookup, agencies AgencyStore, suppliers SupplierAccounts, txm tx.Runner) *ReconService {
	return &ReconService{Base: b, bsp: bsp, letters: letters, bookings: bookings, agencies: agencies, suppliers: suppliers, tx: txm}
}

// StatementInput is an imported BSP billing statement.
type StatementInput struct {
	Label       string          `json:"label"`
	PeriodStart string          `json:"period_start"`
	PeriodEnd   string          `json:"period_end"`
	Currency    string          `json:"currency"`
	Lines       []StatementLine `json:"lines"`
}

// StatementLine is one document on the statement.
type StatementLine struct {
	DocumentNo string `json:"document_no"`
	PNR        string `json:"pnr"`
	Type       string `json:"type"`
	Passenger  string `json:"passenger"`
	IssuedOn   string `json:"issued_on"`
	Amount     int64  `json:"amount"`
}

// ImportStatement stores a statement and its cross-check against the air
// bookings issued in the period.
func (s *ReconService) ImportStatement(ctx context.Context, branchID, actor uuid.UUID, in StatementInput) (*domain.BSPStatement, []domain.BSPLine, error) {
	f := map[string]any{}
	from, err := domain.ParseDay(in.PeriodStart)
	if err != nil {
		f["period_start"] = "YYYY-MM-DD"
	}
	to, err := domain.ParseDay(in.PeriodEnd)
	if err != nil {
		f["period_end"] = "YYYY-MM-DD"
	}
	cur, ok := domain.NormalizeCurrency(in.Currency)
	if !ok {
		f["currency"] = "ISO 4217 code"
	}
	if len(f) == 0 && (to.Before(from) || to.Sub(from).Hours() > 24*62) {
		f["period_end"] = "period must be 1-62 days"
	}
	if len(strings.TrimSpace(in.Label)) > 120 {
		f["label"] = "too long"
	}
	if len(f) > 0 {
		e := shared.NewValidation("invalid statement")
		e.Details = f
		return nil, nil, e
	}
	lines := make([]domain.BSPLine, len(in.Lines))
	for i, l := range in.Lines {
		lines[i] = domain.BSPLine{ID: uuid.New(), DocumentNo: l.DocumentNo, PNR: l.PNR, Type: l.Type, Passenger: l.Passenger, Amount: l.Amount}
		if l.IssuedOn != "" {
			if d, err := domain.ParseDay(l.IssuedOn); err == nil {
				lines[i].IssuedOn = &d
			}
		}
	}
	if err := domain.NormalizeLines(lines); err != nil {
		return nil, nil, err
	}
	st := &domain.BSPStatement{
		ID: uuid.New(), BranchID: branchID, Label: strings.TrimSpace(in.Label), PeriodStart: from, PeriodEnd: to,
		Currency: cur, CreatedBy: &actor, CreatedAt: s.now(),
	}
	for i := range lines {
		lines[i].StatementID = st.ID
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		tickets, err := s.bsp.SystemTickets(ctx, branchID, from, to.AddDate(0, 0, 1), cur)
		if err != nil {
			return err
		}
		lines = domain.Reconcile(st, lines, tickets)
		if err := s.bsp.InsertStatement(ctx, st, lines); err != nil {
			return err
		}
		return s.record(ctx, actor, "bsp.statement_imported", "bsp_statement", st.ID, branchID, map[string]any{
			"lines": st.LineCount, "matched": st.Matched, "mismatched": st.Mismatched,
			"missing_system": st.MissingSystem, "missing_bsp": st.MissingBSP, "discrepancy": st.Discrepancy(),
		})
	})
	if err != nil {
		return nil, nil, err
	}
	return st, lines, nil
}

func (s *ReconService) Statements(ctx context.Context, branchID *uuid.UUID) ([]domain.BSPStatement, error) {
	return s.bsp.ListStatements(ctx, branchID, 50)
}

func (s *ReconService) Statement(ctx context.Context, id uuid.UUID) (*domain.BSPStatement, []domain.BSPLine, error) {
	return s.bsp.FindStatement(ctx, id)
}

// RefundQuote is the settlement plus the booking it was drawn from.
type RefundQuote struct {
	Input      domain.RefundInput
	Settlement domain.RefundSettlement
	Currency   string
	BookingID  *uuid.UUID
}

// RefundQuoteInput describes a cancellation; with a booking, missing
// figures come from the booking (paid so far and supplier cost).
type RefundQuoteInput struct {
	BookingID       *uuid.UUID `json:"booking_id"`
	Currency        string     `json:"currency"`
	Paid            *int64     `json:"paid"`
	SupplierCost    *int64     `json:"supplier_cost"`
	SupplierPenalty int64      `json:"supplier_penalty"`
	ServiceFee      int64      `json:"service_fee"`
}

func (s *ReconService) QuoteRefund(ctx context.Context, in RefundQuoteInput) (*RefundQuote, error) {
	q := &RefundQuote{Currency: in.Currency, BookingID: in.BookingID}
	ri := domain.RefundInput{SupplierPenalty: in.SupplierPenalty, ServiceFee: in.ServiceFee}
	if in.BookingID != nil {
		b, err := s.bookings.BookingDue(ctx, *in.BookingID)
		if err != nil {
			return nil, err
		}
		q.Currency = b.Currency
		ri.Paid, ri.SupplierCost = b.Collected, b.Cost
	}
	if in.Paid != nil {
		ri.Paid = *in.Paid
	}
	if in.SupplierCost != nil {
		ri.SupplierCost = *in.SupplierCost
	}
	cur, ok := domain.NormalizeCurrency(q.Currency)
	if !ok {
		return nil, shared.NewValidation("currency is required")
	}
	q.Currency = cur
	if err := ri.Validate(); err != nil {
		return nil, err
	}
	q.Input, q.Settlement = ri, domain.SettleRefund(ri)
	return q, nil
}

// LetterInput requests a balance confirmation.
type LetterInput struct {
	PartyType string    `json:"party_type"`
	PartyID   uuid.UUID `json:"party_id"`
	Email     string    `json:"email"`
}

// CreateLetter freezes the party's current balance and returns the
// letter with its one-time secret (only the hash is stored).
func (s *ReconService) CreateLetter(ctx context.Context, branchScope *uuid.UUID, actor uuid.UUID, in LetterInput) (*domain.Letter, string, error) {
	l := &domain.Letter{ID: uuid.New(), PartyType: in.PartyType, PartyID: in.PartyID, Status: domain.LetterSent, CreatedBy: &actor}
	switch in.PartyType {
	case domain.PartyAgency:
		a, err := s.agencies.FindAgency(ctx, in.PartyID)
		if err != nil {
			return nil, "", err
		}
		exp, err := s.agencies.Exposures(ctx, &a.BranchID, s.today())
		if err != nil {
			return nil, "", err
		}
		l.BranchID, l.PartyName, l.Currency, l.Email = a.BranchID, a.Name, a.Currency, a.Email
		l.Balance = exp[a.ID].Outstanding
	case domain.PartySupplier:
		sup, err := s.suppliers.Get(ctx, in.PartyID)
		if err != nil {
			return nil, "", err
		}
		l.BranchID, l.PartyName, l.Currency, l.Email = sup.BranchID, sup.NameEn, sup.Finance.Currency, sup.ContactEmail
		switch sup.Finance.Model {
		case supplierdomain.PaymentPrepaid:
			l.Balance = sup.Finance.DepositBalance
		case supplierdomain.PaymentPostpaid:
			l.Balance = -sup.Finance.CreditUsed
		}
	default:
		return nil, "", shared.NewValidation("party_type must be supplier or agency")
	}
	if branchScope != nil && *branchScope != l.BranchID {
		return nil, "", access.ErrBranchForbidden
	}
	if e := strings.TrimSpace(in.Email); e != "" {
		l.Email = e
	}
	if len(l.Email) > 200 {
		return nil, "", shared.NewValidation("email too long")
	}
	token, hash, err := domain.NewLetterToken()
	if err != nil {
		return nil, "", err
	}
	now := s.now()
	l.TokenHash, l.PeriodEnd, l.CreatedAt, l.ExpiresAt = hash, s.today(), now, now.Add(domain.LetterTTL)
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.letters.InsertLetter(ctx, l); err != nil {
			return err
		}
		return s.record(ctx, actor, "reconciliation.letter_sent", "reconciliation_letter", l.ID, l.BranchID, map[string]any{
			"party_type": l.PartyType, "party_id": l.PartyID, "balance": l.Balance, "currency": l.Currency,
		})
	})
	if err != nil {
		return nil, "", err
	}
	return l, token, nil
}

func (s *ReconService) Letters(ctx context.Context, branchID *uuid.UUID) ([]domain.Letter, error) {
	return s.letters.ListLetters(ctx, branchID, 100)
}

// PublicLetter is what the counterparty sees behind the link.
type PublicLetter struct {
	Letter  domain.Letter
	Company string
}

// LetterByToken resolves a public link (no session: system scope).
func (s *ReconService) LetterByToken(ctx context.Context, token string) (*PublicLetter, error) {
	ctx = access.WithScope(ctx, access.System())
	if len(token) < 32 || len(token) > 64 {
		return nil, shared.NewNotFound("confirmation request")
	}
	l, err := s.letters.FindLetterByToken(ctx, domain.HashLetterToken(token))
	if err != nil {
		return nil, err
	}
	company, err := s.letters.BranchName(ctx, l.BranchID)
	if err != nil {
		return nil, err
	}
	return &PublicLetter{Letter: *l, Company: company}, nil
}

// LetterResponse is the counterparty's answer.
type LetterResponse struct {
	Confirm bool   `json:"confirm"`
	Name    string `json:"name"`
	Note    string `json:"note"`
}

func (s *ReconService) RespondLetter(ctx context.Context, token string, in LetterResponse) (*PublicLetter, error) {
	pl, err := s.LetterByToken(ctx, token)
	if err != nil {
		return nil, err
	}
	ctx = access.WithScope(ctx, access.System())
	l := &pl.Letter
	if err := l.Respond(in.Confirm, in.Name, in.Note, s.now()); err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.letters.SaveLetterResponse(ctx, l); err != nil {
			return err
		}
		return s.record(ctx, uuid.Nil, "reconciliation.letter_answered", "reconciliation_letter", l.ID, l.BranchID, map[string]any{
			"status": l.Status, "by": l.RespondedBy,
		})
	})
	if err != nil {
		return nil, err
	}
	return pl, nil
}
