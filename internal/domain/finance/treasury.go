package finance

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Treasury account kinds.
const (
	AccountCash   = "cash"
	AccountBank   = "bank"
	AccountPOS    = "pos"
	AccountWallet = "wallet"
)

var AccountKinds = []string{AccountCash, AccountBank, AccountPOS, AccountWallet}

// Account is a place money sits: a till (kasa), a bank account, a virtual
// POS merchant account or a digital wallet. Balance is maintained by
// movements only.
type Account struct {
	ID                  uuid.UUID
	BranchID            uuid.UUID
	Kind                string
	Name                string
	Currency            string
	BankName            string
	IBAN                string
	CommissionBPS       int
	Balance             int64
	LowBalanceThreshold int64
	IsActive            bool
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// MaxCommissionBPS caps a POS commission rate at 20 %.
const MaxCommissionBPS = 2_000

// Normalize trims and validates the editable fields.
func (a *Account) Normalize() error {
	f := fields{}
	a.Kind = strings.ToLower(strings.TrimSpace(a.Kind))
	a.Name = strings.TrimSpace(a.Name)
	a.BankName = strings.TrimSpace(a.BankName)
	a.IBAN = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(a.IBAN), " ", ""))
	cur, ok := NormalizeCurrency(a.Currency)
	a.Currency = cur
	if !slices.Contains(AccountKinds, a.Kind) {
		f.add("kind", "cash, bank, pos or wallet")
	}
	if a.Name == "" || len(a.Name) > 120 {
		f.add("name", "1-120 characters")
	}
	if !ok {
		f.add("currency", "ISO 4217 code")
	}
	if a.IBAN != "" && !validIBAN(a.IBAN) {
		f.add("iban", "invalid IBAN")
	}
	if a.Kind != AccountPOS {
		a.CommissionBPS = 0
	}
	if a.CommissionBPS < 0 || a.CommissionBPS > MaxCommissionBPS {
		f.add("commission_bps", "0-2000")
	}
	if a.LowBalanceThreshold < 0 || a.LowBalanceThreshold > MaxMoney {
		f.add("low_balance_threshold", "out of range")
	}
	return f.err("invalid account")
}

// LowBalance is true when a threshold is set and the balance is below it.
func (a Account) LowBalance() bool {
	return a.LowBalanceThreshold > 0 && a.Balance < a.LowBalanceThreshold
}

// validIBAN applies the ISO 13616 mod-97 check.
func validIBAN(iban string) bool {
	if len(iban) < 15 || len(iban) > 34 {
		return false
	}
	rearranged := iban[4:] + iban[:4]
	rem := 0
	for _, r := range rearranged {
		switch {
		case r >= '0' && r <= '9':
			rem = (rem*10 + int(r-'0')) % 97
		case r >= 'A' && r <= 'Z':
			v := int(r-'A') + 10
			rem = (rem*100 + v) % 97
		default:
			return false
		}
	}
	return rem == 1
}

// Movement directions.
const (
	DirectionIn  = "in"
	DirectionOut = "out"
)

// Movement kinds.
const (
	MoveCollection      = "collection"
	MoveSupplierPayment = "supplier_payment"
	MoveTransfer        = "transfer"
	MoveExpense         = "expense"
	MoveRefund          = "refund"
	MoveAdjustment      = "adjustment"
)

var MoveKinds = []string{MoveCollection, MoveSupplierPayment, MoveTransfer, MoveExpense, MoveRefund, MoveAdjustment}

// Movement sources.
const (
	SourceManual   = "manual"
	SourceBankFeed = "bank_feed"
)

// Bank feed match states (manual movements are always "na").
const (
	MatchNA        = "na"
	MatchUnmatched = "unmatched"
	MatchMatched   = "matched"
	MatchIgnored   = "ignored"
)

// Movement is an append-only money movement on one account. Amount is the
// gross figure; Fee is what the bank or POS kept, so the balance moves by
// Amount-Fee on the way in and by Amount+Fee on the way out.
type Movement struct {
	ID               uuid.UUID
	AccountID        uuid.UUID
	BranchID         uuid.UUID
	Direction        string
	Kind             string
	Amount           int64
	Fee              int64
	Currency         string
	BalanceAfter     int64
	BookingID        *uuid.UUID
	SupplierID       *uuid.UUID
	CounterAccountID *uuid.UUID
	TransferID       *uuid.UUID
	Source           string
	ExternalID       string
	Reference        string
	Counterparty     string
	Note             string
	MatchStatus      string
	MatchedPaymentID *uuid.UUID
	OccurredOn       time.Time
	ActorID          *uuid.UUID
	CreatedAt        time.Time
}

// Net is the signed effect on the account balance.
func (m Movement) Net() int64 {
	if m.Direction == DirectionIn {
		return m.Amount - m.Fee
	}
	return -(m.Amount + m.Fee)
}

// Validate checks a movement before it is applied.
func (m *Movement) Validate() error {
	f := fields{}
	m.Direction = strings.ToLower(strings.TrimSpace(m.Direction))
	m.Kind = strings.ToLower(strings.TrimSpace(m.Kind))
	m.Reference = strings.TrimSpace(m.Reference)
	m.Counterparty = strings.TrimSpace(m.Counterparty)
	m.Note = strings.TrimSpace(m.Note)
	m.ExternalID = strings.TrimSpace(m.ExternalID)
	if m.Direction != DirectionIn && m.Direction != DirectionOut {
		f.add("direction", "in or out")
	}
	if !slices.Contains(MoveKinds, m.Kind) {
		f.add("kind", "unknown movement kind")
	}
	if !inRange(m.Amount) {
		f.add("amount", "must be positive")
	}
	if m.Fee < 0 || m.Fee > m.Amount {
		f.add("fee", "between 0 and the amount")
	}
	if len(m.Reference) > 140 || len(m.Note) > 500 || len(m.Counterparty) > 140 || len(m.ExternalID) > 120 {
		f.add("reference", "too long")
	}
	switch m.Kind {
	case MoveCollection:
		if m.Direction != DirectionIn {
			f.add("direction", "collections come in")
		}
	case MoveSupplierPayment, MoveExpense, MoveRefund:
		if m.Direction != DirectionOut {
			f.add("direction", "this kind goes out")
		}
	}
	if m.Source == "" {
		m.Source = SourceManual
	}
	if m.Source != SourceManual && m.Source != SourceBankFeed {
		f.add("source", "manual or bank_feed")
	}
	if m.MatchStatus == "" {
		m.MatchStatus = MatchNA
		if m.Source == SourceBankFeed && m.Direction == DirectionIn && m.BookingID == nil {
			m.MatchStatus = MatchUnmatched
		}
	}
	return f.err("invalid movement")
}

// Post applies a validated movement to the account and stamps the running
// balance. Cash tills and wallets may not go negative; bank and POS
// accounts mirror the bank, which is the source of truth, so they may.
func (a *Account) Post(m *Movement) error {
	if !a.IsActive {
		return shared.NewInvalidState("account is inactive")
	}
	if m.Currency == "" {
		m.Currency = a.Currency
	}
	if m.Currency != a.Currency {
		return fieldErr("currency", "movement currency must match the account")
	}
	next := a.Balance + m.Net()
	if next < 0 && (a.Kind == AccountCash || a.Kind == AccountWallet) {
		e := shared.NewConflict("balance is not enough for this movement")
		e.Details = map[string]any{"amount": "balance is not enough"}
		return e
	}
	if next > MaxMoney || next < -MaxMoney {
		return fieldErr("amount", "balance out of range")
	}
	a.Balance = next
	m.AccountID, m.BranchID, m.BalanceAfter = a.ID, a.BranchID, next
	return nil
}

// POSFee is the commission a POS account keeps on a gross collection.
func (a Account) POSFee(amount int64) int64 {
	if a.Kind != AccountPOS || a.CommissionBPS <= 0 {
		return 0
	}
	return ApplyBPS(amount, a.CommissionBPS)
}
