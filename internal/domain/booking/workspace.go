package booking

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Service types classify what a booking primarily sells.
const (
	ServiceFlight   = "flight"
	ServiceHotel    = "hotel"
	ServicePackage  = "package"
	ServiceTransfer = "transfer"
	ServiceVisa     = "visa"
	ServiceTour     = "tour"
)

func ServiceTypes() []string {
	return []string{ServiceFlight, ServiceHotel, ServicePackage, ServiceTransfer, ServiceVisa, ServiceTour}
}

// Supplier sources name where inventory was sourced; empty means not recorded.
const (
	SourceDuffel         = "duffel"
	SourcePaximum        = "paximum"
	SourceAmadeus        = "amadeus"
	SourceSabre          = "sabre"
	SourceSaadia         = "saadia"
	SourceNusuk          = "nusuk"
	SourceDirectContract = "direct_contract"
	SourceOther          = "other"
)

func SupplierSources() []string {
	return []string{SourceDuffel, SourcePaximum, SourceAmadeus, SourceSabre, SourceSaadia, SourceNusuk, SourceDirectContract, SourceOther}
}

// Sales channels a booking can originate from.
const (
	ChannelB2CWeb      = "b2c_web"
	ChannelB2BAgency   = "b2b_agency"
	ChannelWhatsAppBot = "whatsapp_bot"
	ChannelAgent       = "agent"
)

func Channels() []string {
	return []string{ChannelB2CWeb, ChannelB2BAgency, ChannelWhatsAppBot, ChannelAgent}
}

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

const (
	MaxPNRLen         = 32
	MaxSummaryLen     = 160
	MaxCompanyNameLen = 160
)

var pnrPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]*$`)

// Profile is the commercial identity of a booking: supplier reference,
// classification and origin. It is editable in every status because PNRs
// and supplier references arrive after the sale and matter for refunds.
type Profile struct {
	PNR            string
	ServiceType    string
	SupplierSource string
	Channel        string
	Summary        string
	CompanyName    string
}

// DefaultProfile is the profile of bookings created without one.
func DefaultProfile() Profile {
	return Profile{ServiceType: ServicePackage, Channel: ChannelAgent}
}

// Normalize trims, upper-cases the PNR, applies defaults and validates.
func (p Profile) Normalize() (Profile, error) {
	out := Profile{
		PNR:            strings.ToUpper(strings.TrimSpace(p.PNR)),
		ServiceType:    strings.TrimSpace(p.ServiceType),
		SupplierSource: strings.TrimSpace(p.SupplierSource),
		Channel:        strings.TrimSpace(p.Channel),
		Summary:        strings.Join(strings.Fields(p.Summary), " "),
		CompanyName:    strings.Join(strings.Fields(p.CompanyName), " "),
	}
	if out.ServiceType == "" {
		out.ServiceType = ServicePackage
	}
	if out.Channel == "" {
		out.Channel = ChannelAgent
	}
	fields := map[string]any{}
	if out.PNR != "" && (len(out.PNR) > MaxPNRLen || !pnrPattern.MatchString(out.PNR)) {
		fields["pnr"] = "letters, digits and dashes only (max 32)"
	}
	if !oneOf(out.ServiceType, ServiceTypes()) {
		fields["service_type"] = "unknown service type"
	}
	if out.SupplierSource != "" && !oneOf(out.SupplierSource, SupplierSources()) {
		fields["supplier_source"] = "unknown supplier source"
	}
	if !oneOf(out.Channel, Channels()) {
		fields["channel"] = "unknown channel"
	}
	if utf8.RuneCountInString(out.Summary) > MaxSummaryLen {
		fields["summary"] = "too long"
	}
	if utf8.RuneCountInString(out.CompanyName) > MaxCompanyNameLen {
		fields["company_name"] = "too long"
	}
	if len(fields) > 0 {
		err := shared.NewValidation("invalid booking profile")
		err.Details = fields
		return Profile{}, err
	}
	return out, nil
}

// Profile returns the booking's current profile.
func (b *Booking) Profile() Profile {
	return Profile{
		PNR: b.PNR, ServiceType: b.ServiceType, SupplierSource: b.SupplierSource,
		Channel: b.Channel, Summary: b.Summary, CompanyName: b.CompanyName,
	}
}

// ApplyProfile replaces the profile; it reports whether anything changed.
func (b *Booking) ApplyProfile(p Profile, now time.Time) (bool, error) {
	n, err := p.Normalize()
	if err != nil {
		return false, err
	}
	if n == b.Profile() {
		return false, nil
	}
	b.PNR, b.ServiceType, b.SupplierSource = n.PNR, n.ServiceType, n.SupplierSource
	b.Channel, b.Summary, b.CompanyName = n.Channel, n.Summary, n.CompanyName
	b.UpdatedAt = now
	return true, nil
}

// RefCode is the human booking number shown to staff and customers.
func RefCode(n int64) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("BK-%06d", n)
}

var refPattern = regexp.MustCompile(`^(?i)BK-?0*([0-9]{1,12})$`)

// ParseRefCode accepts "BK-000123", "bk123" or "BK-123".
func ParseRefCode(s string) (int64, bool) {
	m := refPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	return n, err == nil && n > 0
}

// Ticket statuses summarize fulfilment for operations.
const (
	TicketPending   = "pending"
	TicketOption    = "option"
	TicketIssued    = "issued"
	TicketReissued  = "reissued"
	TicketCancelled = "cancelled"
	TicketRefunded  = "refunded"
)

// TicketStatus derives the fulfilment status from the lifecycle, completed
// change requests and approved refunds.
func TicketStatus(s Status, reissues int, refunded int64) string {
	switch {
	case s == StatusCancelled && refunded > 0:
		return TicketRefunded
	case s == StatusCancelled:
		return TicketCancelled
	case s == StatusOptionHold:
		return TicketOption
	case s.IsSold() && reissues > 0:
		return TicketReissued
	case s.IsSold():
		return TicketIssued
	default:
		return TicketPending
	}
}

// Payment statuses summarize collection.
const (
	PaymentNone     = "none"
	PaymentAwaiting = "awaiting"
	PaymentDeposit  = "deposit"
	PaymentPaid     = "paid"
	PaymentOverdue  = "overdue"
)

// PaymentStatus derives the collection status; overdue means an open
// payment schedule is past due while a balance remains.
func PaymentStatus(total, collected, balance int64, overdueSchedule bool) string {
	switch {
	case total <= 0 && collected <= 0:
		return PaymentNone
	case balance <= 0:
		return PaymentPaid
	case overdueSchedule:
		return PaymentOverdue
	case collected > 0:
		return PaymentDeposit
	default:
		return PaymentAwaiting
	}
}

// ExtendHold moves the option deadline later. The new deadline must be in
// the future, later than the current one and inside the hold window.
func (b *Booking) ExtendHold(until, now time.Time) (time.Time, error) {
	if b.Status != StatusOptionHold || b.HoldExpiresAt == nil {
		return time.Time{}, shared.NewInvalidState("only bookings on option can be extended")
	}
	prev := *b.HoldExpiresAt
	until = until.UTC()
	switch {
	case !until.After(now):
		return time.Time{}, shared.NewValidation("hold_expires_at must be in the future")
	case !until.After(prev):
		return time.Time{}, shared.NewValidation("hold_expires_at must be later than the current deadline")
	case until.Sub(now) > MaxHoldDuration:
		return time.Time{}, shared.NewValidation("hold_expires_at is beyond the maximum option window")
	}
	b.HoldExpiresAt = &until
	b.UpdatedAt = now
	return prev, nil
}

// CancellationTier charges PenaltyPct of the total when cancelling at
// least MinDays before departure.
type CancellationTier struct {
	MinDays    int `json:"min_days"`
	PenaltyPct int `json:"penalty_pct"`
}

// DefaultCancellationPolicy is ordered from the most to the least lenient.
func DefaultCancellationPolicy() []CancellationTier {
	return []CancellationTier{
		{MinDays: 45, PenaltyPct: 10},
		{MinDays: 30, PenaltyPct: 25},
		{MinDays: 15, PenaltyPct: 50},
		{MinDays: 7, PenaltyPct: 75},
		{MinDays: 0, PenaltyPct: 100},
	}
}

// CancellationQuote is the penalty and refundable amount of a cancellation.
type CancellationQuote struct {
	DaysToDeparture int                `json:"days_to_departure"`
	PenaltyPct      int                `json:"penalty_pct"`
	PenaltyAmt      int64              `json:"penalty_amt"`
	RefundableAmt   int64              `json:"refundable_amt"`
	CollectedAmt    int64              `json:"collected_amt"`
	TotalAmount     int64              `json:"total_amount"`
	Currency        string             `json:"currency"`
	Policy          []CancellationTier `json:"policy"`
}

// QuoteCancellation applies policy to the booking. Only sold bookings carry
// a penalty (options and quotes release for free); past departures and days
// below every tier are charged in full.
func (b *Booking) QuoteCancellation(daysToDeparture int, policy []CancellationTier) CancellationQuote {
	q := CancellationQuote{
		DaysToDeparture: daysToDeparture, CollectedAmt: b.CollectedAmt,
		TotalAmount: b.TotalAmount, Currency: b.Currency, Policy: policy,
	}
	if b.Status.IsSold() {
		q.PenaltyPct = 100
		for _, t := range policy {
			if daysToDeparture >= t.MinDays {
				q.PenaltyPct = t.PenaltyPct
				break
			}
		}
	}
	q.PenaltyAmt = b.TotalAmount * int64(q.PenaltyPct) / 100
	if q.RefundableAmt = b.CollectedAmt - q.PenaltyAmt; q.RefundableAmt < 0 {
		q.RefundableAmt = 0
	}
	return q
}

// DaysUntil counts calendar days from today to day (negative when past).
func DaysUntil(day, today time.Time) int {
	d := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	t := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return int(d.Sub(t).Hours() / 24)
}
