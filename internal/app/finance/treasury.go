package finance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// MaxFeedRows bounds one bank statement import.
const MaxFeedRows = 2_000

// TreasuryService runs tills, bank and POS accounts and the bank feed.
type TreasuryService struct {
	Base
	store    TreasuryStore
	bookings BookingLookup
	payments PaymentRecorder
	tx       tx.Runner
}

func NewTreasuryService(b Base, store TreasuryStore, bookings BookingLookup, payments PaymentRecorder, txm tx.Runner) *TreasuryService {
	return &TreasuryService{Base: b, store: store, bookings: bookings, payments: payments, tx: txm}
}

// AccountInput is the editable part of an account.
type AccountInput struct {
	Kind                string `json:"kind"`
	Name                string `json:"name"`
	Currency            string `json:"currency"`
	BankName            string `json:"bank_name"`
	IBAN                string `json:"iban"`
	CommissionBPS       int    `json:"commission_bps"`
	LowBalanceThreshold int64  `json:"low_balance_threshold"`
	IsActive            *bool  `json:"is_active"`
	OpeningBalance      int64  `json:"opening_balance"`
}

func (s *TreasuryService) Accounts(ctx context.Context, branchID *uuid.UUID) ([]domain.Account, error) {
	return s.store.ListAccounts(ctx, branchID)
}

// CreateAccount opens an account; an opening balance is booked as an
// adjustment movement so the ledger always explains the balance.
func (s *TreasuryService) CreateAccount(ctx context.Context, branchID, actor uuid.UUID, in AccountInput) (*domain.Account, error) {
	now := s.now()
	a := &domain.Account{
		ID: uuid.New(), BranchID: branchID, Kind: in.Kind, Name: in.Name, Currency: in.Currency,
		BankName: in.BankName, IBAN: in.IBAN, CommissionBPS: in.CommissionBPS,
		LowBalanceThreshold: in.LowBalanceThreshold, IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.Normalize(); err != nil {
		return nil, err
	}
	if in.OpeningBalance < 0 || in.OpeningBalance > domain.MaxMoney {
		return nil, shared.NewValidation("opening balance out of range")
	}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.store.InsertAccount(ctx, a); err != nil {
			return err
		}
		if in.OpeningBalance > 0 {
			m := &domain.Movement{
				ID: uuid.New(), Direction: domain.DirectionIn, Kind: domain.MoveAdjustment, Amount: in.OpeningBalance,
				Note: "Opening balance", OccurredOn: s.today(), ActorID: &actor, CreatedAt: now,
			}
			if err := s.post(ctx, a, m); err != nil {
				return err
			}
		}
		return s.record(ctx, actor, "treasury.account_created", "treasury_account", a.ID, branchID, a)
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// UpdateAccount edits the descriptive fields; currency and kind are fixed
// once money moved through the account.
func (s *TreasuryService) UpdateAccount(ctx context.Context, id, actor uuid.UUID, in AccountInput) (*domain.Account, error) {
	var out *domain.Account
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		a, err := s.store.LockAccount(ctx, id)
		if err != nil {
			return err
		}
		a.Name, a.BankName, a.IBAN = in.Name, in.BankName, in.IBAN
		a.CommissionBPS, a.LowBalanceThreshold = in.CommissionBPS, in.LowBalanceThreshold
		if in.IsActive != nil {
			a.IsActive = *in.IsActive
		}
		if err := a.Normalize(); err != nil {
			return err
		}
		a.UpdatedAt = s.now()
		if err := s.store.UpdateAccount(ctx, a); err != nil {
			return err
		}
		out = a
		return s.record(ctx, actor, "treasury.account_updated", "treasury_account", a.ID, a.BranchID, a)
	})
	return out, err
}

// MovementInput records money in or out of an account.
type MovementInput struct {
	AccountID    uuid.UUID  `json:"-"`
	Direction    string     `json:"direction"`
	Kind         string     `json:"kind"`
	Amount       int64      `json:"amount"`
	Fee          *int64     `json:"fee"`
	BookingID    *uuid.UUID `json:"booking_id"`
	SupplierID   *uuid.UUID `json:"supplier_id"`
	Reference    string     `json:"reference"`
	Counterparty string     `json:"counterparty"`
	Note         string     `json:"note"`
	OccurredOn   string     `json:"occurred_on"`
}

func (s *TreasuryService) occurredOn(v string) (time.Time, error) {
	if v == "" {
		return s.today(), nil
	}
	d, err := domain.ParseDay(v)
	if err != nil {
		return time.Time{}, shared.NewValidation("occurred_on must be YYYY-MM-DD")
	}
	if d.After(s.today()) {
		return time.Time{}, shared.NewValidation("occurred_on cannot be in the future")
	}
	return d, nil
}

// Post records one manual movement. A POS collection without an explicit
// fee is charged the account's commission rate.
func (s *TreasuryService) Post(ctx context.Context, actor uuid.UUID, in MovementInput) (*domain.Movement, *domain.Account, error) {
	day, err := s.occurredOn(in.OccurredOn)
	if err != nil {
		return nil, nil, err
	}
	if in.Kind == domain.MoveTransfer {
		return nil, nil, shared.NewValidation("use the transfer endpoint for transfers")
	}
	var m *domain.Movement
	var acc *domain.Account
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		a, err := s.store.LockAccount(ctx, in.AccountID)
		if err != nil {
			return err
		}
		fee := int64(0)
		if in.Fee != nil {
			fee = *in.Fee
		} else if in.Direction == domain.DirectionIn && in.Kind == domain.MoveCollection {
			fee = a.POSFee(in.Amount)
		}
		m = &domain.Movement{
			ID: uuid.New(), Direction: in.Direction, Kind: in.Kind, Amount: in.Amount, Fee: fee,
			BookingID: in.BookingID, SupplierID: in.SupplierID, Reference: in.Reference, Counterparty: in.Counterparty,
			Note: in.Note, OccurredOn: day, ActorID: &actor, CreatedAt: s.now(),
		}
		if err := m.Validate(); err != nil {
			return err
		}
		if m.BookingID != nil {
			if err := s.checkBookingBranch(ctx, *m.BookingID, a.BranchID); err != nil {
				return err
			}
		}
		if err := s.post(ctx, a, m); err != nil {
			return err
		}
		acc = a
		return s.record(ctx, actor, "treasury.movement_posted", "treasury_movement", m.ID, a.BranchID, m)
	})
	return m, acc, err
}

func (s *TreasuryService) checkBookingBranch(ctx context.Context, bookingID, branchID uuid.UUID) error {
	b, err := s.bookings.BookingDue(ctx, bookingID)
	if err != nil {
		return err
	}
	if b.BranchID != branchID {
		return shared.NewValidation("booking belongs to another branch")
	}
	return nil
}

// post applies a movement to a locked account and persists both.
func (s *TreasuryService) post(ctx context.Context, a *domain.Account, m *domain.Movement) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if err := a.Post(m); err != nil {
		return err
	}
	inserted, err := s.store.InsertMovement(ctx, m)
	if err != nil {
		return err
	}
	if !inserted {
		return errDuplicate
	}
	return s.store.SetBalance(ctx, a.ID, a.Balance)
}

var errDuplicate = shared.NewConflict("movement already imported")

// TransferInput moves money between two accounts of the same currency.
type TransferInput struct {
	FromID     uuid.UUID `json:"from_account_id"`
	ToID       uuid.UUID `json:"to_account_id"`
	Amount     int64     `json:"amount"`
	Fee        int64     `json:"fee"`
	Note       string    `json:"note"`
	OccurredOn string    `json:"occurred_on"`
}

// Transfer books a paired out/in movement atomically. Accounts are locked
// in id order so concurrent opposite transfers cannot deadlock.
func (s *TreasuryService) Transfer(ctx context.Context, actor uuid.UUID, in TransferInput) ([]domain.Movement, error) {
	if in.FromID == in.ToID {
		return nil, shared.NewValidation("choose two different accounts")
	}
	day, err := s.occurredOn(in.OccurredOn)
	if err != nil {
		return nil, err
	}
	var out []domain.Movement
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		first, second := in.FromID, in.ToID
		if first.String() > second.String() {
			first, second = second, first
		}
		a1, err := s.store.LockAccount(ctx, first)
		if err != nil {
			return err
		}
		a2, err := s.store.LockAccount(ctx, second)
		if err != nil {
			return err
		}
		from, to := a1, a2
		if a1.ID != in.FromID {
			from, to = a2, a1
		}
		if from.Currency != to.Currency {
			return shared.NewValidation("transfers need two accounts in the same currency")
		}
		if from.BranchID != to.BranchID {
			return shared.NewValidation("transfers stay within one branch")
		}
		transferID := uuid.New()
		now := s.now()
		outM := &domain.Movement{
			ID: uuid.New(), Direction: domain.DirectionOut, Kind: domain.MoveTransfer, Amount: in.Amount, Fee: in.Fee,
			CounterAccountID: &to.ID, TransferID: &transferID, Note: in.Note, OccurredOn: day, ActorID: &actor, CreatedAt: now,
		}
		inM := &domain.Movement{
			ID: uuid.New(), Direction: domain.DirectionIn, Kind: domain.MoveTransfer, Amount: in.Amount,
			CounterAccountID: &from.ID, TransferID: &transferID, Note: in.Note, OccurredOn: day, ActorID: &actor, CreatedAt: now,
		}
		if err := s.post(ctx, from, outM); err != nil {
			return err
		}
		if err := s.post(ctx, to, inM); err != nil {
			return err
		}
		out = []domain.Movement{*outM, *inM}
		return s.record(ctx, actor, "treasury.transfer", "treasury_movement", transferID, from.BranchID, out)
	})
	return out, err
}

func (s *TreasuryService) Movements(ctx context.Context, f MovementFilter) ([]domain.Movement, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	return s.store.ListMovements(ctx, f)
}

// FeedRow is one line of a bank statement (open-banking export or CSV).
type FeedRow struct {
	ExternalID   string `json:"external_id"`
	OccurredOn   string `json:"occurred_on"`
	Amount       int64  `json:"amount"`
	Direction    string `json:"direction"`
	Description  string `json:"description"`
	Counterparty string `json:"counterparty"`
}

// FeedResult summarizes an import.
type FeedResult struct {
	Imported    int `json:"imported"`
	Duplicates  int `json:"duplicates"`
	AutoMatched int `json:"auto_matched"`
	Unmatched   int `json:"unmatched"`
}

// ImportFeed books bank statement lines once (deduplicated on the bank's
// transaction id) and settles incoming credits that name exactly one open
// booking, recording the customer payment on it.
func (s *TreasuryService) ImportFeed(ctx context.Context, accountID, actor uuid.UUID, mayApprove bool, rows []FeedRow) (*FeedResult, error) {
	if len(rows) == 0 || len(rows) > MaxFeedRows {
		return nil, shared.NewValidation(fmt.Sprintf("1-%d rows", MaxFeedRows))
	}
	acc, err := s.store.FindAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if acc.Kind != domain.AccountBank && acc.Kind != domain.AccountPOS {
		return nil, shared.NewValidation("bank feeds import into bank or POS accounts")
	}
	for i, r := range rows {
		if r.ExternalID == "" {
			return nil, shared.NewValidation(fmt.Sprintf("row %d: external_id is required for de-duplication", i+1))
		}
	}
	res := &FeedResult{}
	var credits []domain.Movement
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		a, err := s.store.LockAccount(ctx, accountID)
		if err != nil {
			return err
		}
		for i, r := range rows {
			day, err := s.occurredOn(r.OccurredOn)
			if err != nil {
				return shared.NewValidation(fmt.Sprintf("row %d: %v", i+1, err))
			}
			kind := domain.MoveCollection
			if r.Direction == domain.DirectionOut {
				kind = domain.MoveExpense
			}
			m := &domain.Movement{
				ID: uuid.New(), Direction: r.Direction, Kind: kind, Amount: r.Amount, Source: domain.SourceBankFeed,
				ExternalID: r.ExternalID, Reference: truncate(r.Description, 140), Counterparty: truncate(r.Counterparty, 140),
				OccurredOn: day, ActorID: &actor, CreatedAt: s.now(),
			}
			if err := m.Validate(); err != nil {
				return shared.NewValidation(fmt.Sprintf("row %d: %v", i+1, err))
			}
			if err := s.post(ctx, a, m); err != nil {
				if errors.Is(err, errDuplicate) {
					res.Duplicates++
					// Roll the in-memory balance back; the row was not stored.
					a.Balance -= m.Net()
					continue
				}
				return err
			}
			res.Imported++
			if m.MatchStatus == domain.MatchUnmatched {
				credits = append(credits, *m)
			}
		}
		return s.record(ctx, actor, "treasury.feed_imported", "treasury_account", a.ID, a.BranchID, res)
	})
	if err != nil {
		return nil, err
	}
	for i := range credits {
		ok, err := s.autoMatch(ctx, acc, &credits[i], actor, mayApprove)
		if err != nil {
			s.logger().WarnContext(ctx, "bank feed auto-match failed", "movement", credits[i].ID, "err", err)
		}
		if ok {
			res.AutoMatched++
		} else {
			res.Unmatched++
		}
	}
	return res, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func (s *TreasuryService) autoMatch(ctx context.Context, acc *domain.Account, m *domain.Movement, actor uuid.UUID, mayApprove bool) (bool, error) {
	refs, pnrs := domain.ExtractRefs(m.Reference + " " + m.Counterparty)
	if len(refs) == 0 && len(pnrs) == 0 {
		return false, nil
	}
	cands, err := s.bookings.BookingsByRefs(ctx, acc.BranchID, refs, pnrs)
	if err != nil {
		return false, err
	}
	hit, ok := domain.MatchCredit(m.Amount, m.Currency, cands)
	if !ok {
		return false, nil
	}
	if _, err := s.settle(ctx, m, hit.BookingID, actor, mayApprove); err != nil {
		return false, err
	}
	return true, nil
}

// settle records the payment for a bank credit and marks it matched.
func (s *TreasuryService) settle(ctx context.Context, m *domain.Movement, bookingID, actor uuid.UUID, mayApprove bool) (*domain.Movement, error) {
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		received := m.OccurredOn
		p, err := s.payments.Record(ctx, apppayment.RecordInput{
			BookingID: bookingID, Amount: m.Amount, Currency: m.Currency, Method: "bank_transfer",
			Reference: truncate(m.Reference, 120), Note: "Bank feed " + m.ExternalID, RecordedBy: actor,
			IdempotencyKey: "treasury:" + m.ID.String(), AutoVerify: true, MayApprove: mayApprove, ReceivedAt: &received,
		})
		if err != nil {
			return err
		}
		if err := s.store.ResolveMatch(ctx, m.ID, domain.MatchMatched, &bookingID, &p.ID); err != nil {
			return err
		}
		m.MatchStatus, m.BookingID, m.MatchedPaymentID = domain.MatchMatched, &bookingID, &p.ID
		return s.record(ctx, actor, "treasury.credit_matched", "treasury_movement", m.ID, m.BranchID, map[string]any{
			"booking_id": bookingID, "payment_id": p.ID, "amount": m.Amount, "currency": m.Currency,
		})
	})
	return m, err
}

// Match closes an open account by hand: the bank credit pays the booking.
func (s *TreasuryService) Match(ctx context.Context, movementID, bookingID, actor uuid.UUID, mayApprove bool) (*domain.Movement, error) {
	m, err := s.store.FindMovement(ctx, movementID)
	if err != nil {
		return nil, err
	}
	if m.MatchStatus != domain.MatchUnmatched {
		return nil, shared.NewInvalidState("this movement is not waiting for a match")
	}
	b, err := s.bookings.BookingDue(ctx, bookingID)
	if err != nil {
		return nil, err
	}
	if b.BranchID != m.BranchID {
		return nil, shared.NewValidation("booking belongs to another branch")
	}
	if b.Currency != m.Currency {
		return nil, shared.NewValidation("booking currency differs from the bank credit")
	}
	return s.settle(ctx, m, bookingID, actor, mayApprove)
}

// Ignore marks a credit that is not a booking payment (e.g. interest).
func (s *TreasuryService) Ignore(ctx context.Context, movementID, actor uuid.UUID) error {
	m, err := s.store.FindMovement(ctx, movementID)
	if err != nil {
		return err
	}
	if m.MatchStatus != domain.MatchUnmatched {
		return shared.NewInvalidState("this movement is not waiting for a match")
	}
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.store.ResolveMatch(ctx, m.ID, domain.MatchIgnored, nil, nil); err != nil {
			return err
		}
		return s.record(ctx, actor, "treasury.credit_ignored", "treasury_movement", m.ID, m.BranchID, nil)
	})
}

// POSWindowDays is the commission analysis window.
const POSWindowDays = 30

func (s *TreasuryService) POSStats(ctx context.Context, branchID *uuid.UUID) ([]POSStat, error) {
	return s.store.POSStats(ctx, branchID, s.today().AddDate(0, 0, -POSWindowDays))
}
