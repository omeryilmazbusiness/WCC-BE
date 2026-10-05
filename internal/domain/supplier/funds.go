package supplier

import (
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Payment models.
const (
	PaymentPrepaid  = "prepaid"
	PaymentPostpaid = "postpaid"
	PaymentCard     = "card"
)

var PaymentModels = []string{PaymentPrepaid, PaymentPostpaid, PaymentCard}

// Payment terms.
var PaymentTerms = []string{"on_booking", "net7", "net15", "net30", "weekly"}

// Finance is the money relationship with the supplier, in minor units of
// Currency. Prepaid accounts spend a deposit; postpaid accounts draw on a
// credit line; card accounts pay per booking and carry no balance.
type Finance struct {
	Model               string
	Currency            string
	DepositBalance      int64
	CreditLimit         int64
	CreditUsed          int64
	LowBalanceThreshold int64
	PaymentTerms        string
}

func (fi *Finance) normalize(f fields) {
	fi.Model = strings.ToLower(strings.TrimSpace(fi.Model))
	fi.Currency = strings.ToUpper(strings.TrimSpace(fi.Currency))
	fi.PaymentTerms = strings.ToLower(strings.TrimSpace(fi.PaymentTerms))
	if fi.Model == "" {
		fi.Model = PaymentPostpaid
	}
	if fi.PaymentTerms == "" {
		fi.PaymentTerms = "net30"
	}
	if fi.Currency == "" {
		fi.Currency = DefaultCurrency
	}
	if !slices.Contains(PaymentModels, fi.Model) {
		f.add("payment_model", "prepaid, postpaid or card")
	}
	if !currencyPattern.MatchString(fi.Currency) {
		f.add("currency", "ISO 4217 code")
	}
	if !slices.Contains(PaymentTerms, fi.PaymentTerms) {
		f.add("payment_terms", "unknown payment terms")
	}
	if fi.CreditLimit < 0 || fi.CreditLimit > MaxMoney {
		f.add("credit_limit", "out of range")
	}
	if fi.LowBalanceThreshold < 0 || fi.LowBalanceThreshold > MaxMoney {
		f.add("low_balance_threshold", "out of range")
	}
}

// DefaultCurrency applies when a profile leaves the account currency empty.
const DefaultCurrency = "SAR"

// Available is the money that can still be spent: the deposit (prepaid) or
// the unused credit line (postpaid). limited is false for card accounts and
// for credit lines without a limit (CreditLimit 0).
func (fi Finance) Available() (amount int64, limited bool) {
	switch fi.Model {
	case PaymentPrepaid:
		return fi.DepositBalance, true
	case PaymentPostpaid:
		if fi.CreditLimit <= 0 {
			return 0, false
		}
		return fi.CreditLimit - fi.CreditUsed, true
	default:
		return 0, false
	}
}

// LowBalance is true when a threshold is set and available money is below it.
func (fi Finance) LowBalance() bool {
	avail, limited := fi.Available()
	return limited && fi.LowBalanceThreshold > 0 && avail < fi.LowBalanceThreshold
}

// Exhausted is true when a limited account has nothing left to spend.
func (fi Finance) Exhausted() bool {
	avail, limited := fi.Available()
	return limited && avail <= 0
}

// UsedPct is the share of the credit line in use (postpaid), 0-100.
func (fi Finance) UsedPct() int {
	if fi.Model != PaymentPostpaid || fi.CreditLimit <= 0 {
		return 0
	}
	pct := fi.CreditUsed * 100 / fi.CreditLimit
	return int(min(max(pct, 0), 100))
}

// Ledger entry kinds.
const (
	EntryTopUp      = "topup"
	EntryCharge     = "charge"
	EntryRefund     = "refund"
	EntryPayment    = "payment"
	EntryAdjustment = "adjustment"
)

var EntryKinds = []string{EntryTopUp, EntryCharge, EntryRefund, EntryPayment, EntryAdjustment}

// LedgerEntry is an append-only money movement on the supplier account.
type LedgerEntry struct {
	ID           uuid.UUID
	SupplierID   uuid.UUID
	BranchID     uuid.UUID
	Kind         string
	Amount       int64
	Currency     string
	BalanceAfter int64
	Reference    string
	Note         string
	ActorID      *uuid.UUID
	CreatedAt    time.Time
}

// Apply books a movement on the account and returns Balance after it. Charges beyond the deposit or the credit line are refused: an
// exhausted account must be topped up or settled first.
func (fi *Finance) Apply(kind string, amount int64) (int64, error) {
	if kind != EntryAdjustment && amount <= 0 {
		return 0, amountErr("amount must be positive")
	}
	if amount == 0 || amount > MaxMoney || amount < -MaxMoney {
		return 0, amountErr("amount out of range")
	}
	switch kind {
	case EntryTopUp:
		if fi.Model != PaymentPrepaid {
			return 0, shared.NewInvalidState("top-ups apply to prepaid accounts")
		}
		fi.DepositBalance += amount
	case EntryCharge:
		switch fi.Model {
		case PaymentPrepaid:
			if amount > fi.DepositBalance {
				return 0, fundsErr("deposit is not enough for this charge")
			}
			fi.DepositBalance -= amount
		case PaymentPostpaid:
			if fi.CreditLimit > 0 && fi.CreditUsed+amount > fi.CreditLimit {
				return 0, fundsErr("credit limit would be exceeded")
			}
			fi.CreditUsed += amount
		}
	case EntryRefund:
		switch fi.Model {
		case PaymentPrepaid:
			fi.DepositBalance += amount
		case PaymentPostpaid:
			if amount > fi.CreditUsed {
				return 0, amountErr("refund is larger than the outstanding amount")
			}
			fi.CreditUsed -= amount
		}
	case EntryPayment:
		if fi.Model != PaymentPostpaid {
			return 0, shared.NewInvalidState("settlements apply to credit-line accounts")
		}
		if amount > fi.CreditUsed {
			return 0, amountErr("payment is larger than the outstanding amount")
		}
		fi.CreditUsed -= amount
	case EntryAdjustment:
		switch fi.Model {
		case PaymentPrepaid:
			if fi.DepositBalance+amount < 0 {
				return 0, amountErr("adjustment would make the deposit negative")
			}
			fi.DepositBalance += amount
		case PaymentPostpaid:
			if fi.CreditUsed+amount < 0 {
				return 0, amountErr("adjustment would make the outstanding amount negative")
			}
			fi.CreditUsed += amount
		default:
			return 0, shared.NewInvalidState("card accounts carry no balance to adjust")
		}
	default:
		return 0, shared.NewValidation("unknown entry kind")
	}
	return fi.Balance(), nil
}

// Balance is the running account figure a ledger row records: the deposit
// left (prepaid), the amount owed on the credit line (postpaid), 0 for cards.
func (fi Finance) Balance() int64 {
	switch fi.Model {
	case PaymentPrepaid:
		return fi.DepositBalance
	case PaymentPostpaid:
		return fi.CreditUsed
	default:
		return 0
	}
}

func amountErr(msg string) error {
	e := shared.NewValidation(msg)
	e.Details = map[string]any{"amount": msg}
	return e
}

func fundsErr(msg string) error {
	e := shared.NewConflict(msg)
	e.Details = map[string]any{"amount": msg}
	return e
}

// Blocking reasons, in the order they are checked.
const (
	BlockInactive        = "inactive"
	BlockContractExpired = "contract_expired"
	BlockDown            = "down"
	BlockDepositEmpty    = "deposit_exhausted"
	BlockCreditFull      = "credit_exhausted"
)

// Warnings that do not block.
const (
	WarnLowBalance       = "low_balance"
	WarnContractExpiring = "contract_expiring"
	WarnDegraded         = "degraded"
)

// Availability says whether the supplier may receive searches and bookings.
type Availability struct {
	Bookable bool     `json:"bookable"`
	Reason   string   `json:"reason"`
	Warnings []string `json:"warnings"`
}

// AvailabilityOn evaluates the critical-balance block and other guards.
func (s *Supplier) AvailabilityOn(today time.Time) Availability {
	a := Availability{Bookable: true, Warnings: []string{}}
	days, hasEnd := s.ContractDaysLeft(today)
	switch {
	case !s.IsActive:
		a.Reason = BlockInactive
	case hasEnd && days < 0:
		a.Reason = BlockContractExpired
	case s.Health.Status == HealthDown:
		a.Reason = BlockDown
	case s.Finance.Model == PaymentPrepaid && s.Finance.Exhausted():
		a.Reason = BlockDepositEmpty
	case s.Finance.Model == PaymentPostpaid && s.Finance.Exhausted():
		a.Reason = BlockCreditFull
	}
	a.Bookable = a.Reason == ""
	if s.Finance.LowBalance() && !s.Finance.Exhausted() {
		a.Warnings = append(a.Warnings, WarnLowBalance)
	}
	if hasEnd && days >= 0 && days <= ContractWarnDays {
		a.Warnings = append(a.Warnings, WarnContractExpiring)
	}
	if s.Health.Status == HealthDegraded {
		a.Warnings = append(a.Warnings, WarnDegraded)
	}
	return a
}
