package finance

import (
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Agency statuses.
const (
	AgencyActive    = "active"
	AgencySuspended = "suspended"
	AgencyClosed    = "closed"
)

var AgencyStatuses = []string{AgencyActive, AgencySuspended, AgencyClosed}

// Suspension reasons.
const (
	SuspendManual  = "manual"
	SuspendOverdue = "overdue"
)

// Agency is a B2B sub-agent that sells on credit. CreditLimit 0 means the
// agency must prepay (no open account).
type Agency struct {
	ID               uuid.UUID
	BranchID         uuid.UUID
	Code             string
	Name             string
	ContactName      string
	Phone            string
	Email            string
	TaxID            string
	Currency         string
	CreditLimit      int64
	PaymentTermsDays int
	GraceDays        int
	AutoSuspend      bool
	Status           string
	SuspendReason    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

var agencyCode = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_-]{1,23}$`)

// Normalize trims and validates the editable fields.
func (a *Agency) Normalize() error {
	f := fields{}
	a.Code = strings.ToUpper(strings.TrimSpace(a.Code))
	a.Name = strings.TrimSpace(a.Name)
	a.ContactName = strings.TrimSpace(a.ContactName)
	a.Phone = strings.TrimSpace(a.Phone)
	a.Email = strings.ToLower(strings.TrimSpace(a.Email))
	a.TaxID = strings.TrimSpace(a.TaxID)
	cur, ok := NormalizeCurrency(a.Currency)
	a.Currency = cur
	if !agencyCode.MatchString(a.Code) {
		f.add("code", "2-24 letters, digits, - or _")
	}
	if a.Name == "" || len(a.Name) > 160 {
		f.add("name", "1-160 characters")
	}
	if a.Email != "" {
		if _, err := mail.ParseAddress(a.Email); err != nil {
			f.add("email", "invalid email")
		}
	}
	if !ok {
		f.add("currency", "ISO 4217 code")
	}
	if a.CreditLimit < 0 || a.CreditLimit > MaxMoney {
		f.add("credit_limit", "out of range")
	}
	if a.PaymentTermsDays < 0 || a.PaymentTermsDays > 180 {
		f.add("payment_terms_days", "0-180")
	}
	if a.GraceDays < 0 || a.GraceDays > 90 {
		f.add("grace_days", "0-90")
	}
	if len(a.ContactName) > 120 || len(a.Phone) > 40 || len(a.TaxID) > 20 {
		f.add("contact_name", "too long")
	}
	return f.err("invalid agency")
}

// Exposure is what an agency owes right now, in its account currency.
type Exposure struct {
	Outstanding        int64
	Overdue            int64
	OldestOverdueDays  int
	OpenBookings       int
	UnconvertedBalance bool
}

// Risk levels.
const (
	RiskOK       = "ok"
	RiskWatch    = "watch"
	RiskCritical = "critical"
	RiskBlocked  = "blocked"
)

// Risk is the credit picture the receivables screen shows per agency.
type Risk struct {
	Available int64
	UsedPct   int
	Level     string
}

// RiskOf grades an agency: blocked when it may not sell, critical above
// 90 % of the limit or with debt past the grace period, watch above 70 %
// or with any overdue debt.
func (a Agency) RiskOf(e Exposure) Risk {
	r := Risk{Available: a.CreditLimit - e.Outstanding}
	if a.CreditLimit > 0 {
		r.UsedPct = int(min(max(e.Outstanding*100/a.CreditLimit, 0), 999))
	}
	switch {
	case a.Status != AgencyActive:
		r.Level = RiskBlocked
	case r.UsedPct >= 90 || (e.Overdue > 0 && e.OldestOverdueDays > a.GraceDays):
		r.Level = RiskCritical
	case r.UsedPct >= 70 || e.Overdue > 0:
		r.Level = RiskWatch
	default:
		r.Level = RiskOK
	}
	return r
}

// ShouldSuspend says whether the overdue sweep closes B2B sales for the agency.
func (a Agency) ShouldSuspend(e Exposure) bool {
	return a.Status == AgencyActive && a.AutoSuspend && e.Overdue > 0 && e.OldestOverdueDays > a.GraceDays
}

// CanSell checks that the agency may take a new booking worth amount on
// credit: active, and the limit (when it sells on credit) still covers it.
func (a Agency) CanSell(e Exposure, amount int64) error {
	switch a.Status {
	case AgencySuspended:
		return shared.NewInvalidState("agency is suspended for overdue debt or by finance")
	case AgencyClosed:
		return shared.NewInvalidState("agency account is closed")
	}
	if a.CreditLimit > 0 && e.Outstanding+amount > a.CreditLimit {
		e := shared.NewConflict("agency credit limit would be exceeded")
		e.Details = map[string]any{"credit_limit": "would be exceeded"}
		return e
	}
	return nil
}

// Suspend closes B2B sales.
func (a *Agency) Suspend(reason string, now time.Time) error {
	if a.Status == AgencyClosed {
		return shared.NewInvalidState("agency account is closed")
	}
	if reason != SuspendManual && reason != SuspendOverdue {
		return fieldErr("reason", "manual or overdue")
	}
	a.Status, a.SuspendReason, a.UpdatedAt = AgencySuspended, reason, now
	return nil
}

// Reactivate reopens sales.
func (a *Agency) Reactivate(now time.Time) error {
	if a.Status == AgencyClosed {
		return shared.NewInvalidState("agency account is closed")
	}
	a.Status, a.SuspendReason, a.UpdatedAt = AgencyActive, "", now
	return nil
}

// Ageing buckets for receivables, by days past the due date.
const (
	AgeCurrent     = "current"
	Age0to15       = "d0_15"
	Age16to30      = "d16_30"
	Age31Plus      = "d31_plus"
	AgeUnscheduled = "unscheduled"
)

var AgeBuckets = []string{AgeCurrent, Age0to15, Age16to30, Age31Plus, AgeUnscheduled}

// BucketOf classifies an amount due on dueOn as seen on today.
func BucketOf(dueOn, today time.Time) string {
	late := DaysBetween(dueOn, today)
	switch {
	case late <= 0:
		return AgeCurrent
	case late <= 15:
		return Age0to15
	case late <= 30:
		return Age16to30
	default:
		return Age31Plus
	}
}

// Ageing sums receivables per bucket in one currency.
type Ageing struct {
	Currency string
	Buckets  map[string]int64
	Counts   map[string]int
}

// NewAgeing starts an empty ageing table.
func NewAgeing(currency string) *Ageing {
	a := &Ageing{Currency: currency, Buckets: map[string]int64{}, Counts: map[string]int{}}
	for _, b := range AgeBuckets {
		a.Buckets[b], a.Counts[b] = 0, 0
	}
	return a
}

// Add books an amount in a bucket.
func (a *Ageing) Add(bucket string, amount int64) {
	a.Buckets[bucket] += amount
	a.Counts[bucket]++
}

// Total is every bucket summed.
func (a *Ageing) Total() int64 {
	var t int64
	for _, v := range a.Buckets {
		t += v
	}
	return t
}

// Overdue is everything past due.
func (a *Ageing) Overdue() int64 {
	return a.Buckets[Age0to15] + a.Buckets[Age16to30] + a.Buckets[Age31Plus]
}

// JobAgenciesSweep suspends agencies whose overdue debt passed their grace days.
const JobAgenciesSweep shared.JobName = "finance.agencies_sweep"
