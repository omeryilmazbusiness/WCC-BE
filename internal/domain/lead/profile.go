package lead

import (
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Segment tells an individual traveller (B2C) from a corporate account (B2B).
type Segment string

const (
	SegmentB2C Segment = "b2c"
	SegmentB2B Segment = "b2b"
)

// Priority is how urgently the owner must act on the lead.
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// Intent is how close the customer is to buying.
type Intent string

const (
	IntentReady     Intent = "ready"     // buying today
	IntentComparing Intent = "comparing" // comparing prices
	IntentPlanning  Intent = "planning"  // planning for later
)

const (
	maxEmailLen       = 254
	maxCompanyLen     = 200
	maxTaxOfficeLen   = 120
	maxFollowUpAhead  = 366 * 24 * time.Hour
	followUpPastSkew  = 5 * time.Minute
	urgentTravelHours = 48
)

var taxNumber = regexp.MustCompile(`^[A-Z0-9-]{4,20}$`)

// Profile is who the lead is and how the pipeline treats it.
type Profile struct {
	Email       string
	Segment     Segment
	CompanyName string // B2B only
	TaxNumber   string // B2B only
	TaxOffice   string // B2B only
	Priority    Priority
	Intent      Intent // empty when unknown
	// NextFollowUpAt is when the owner should contact the customer again.
	NextFollowUpAt *time.Time
}

func ValidPriority(p Priority) bool {
	return p == PriorityHigh || p == PriorityMedium || p == PriorityLow
}

// SuggestedPriority is high when travel starts within two days, else medium.
func SuggestedPriority(travel *time.Time, now time.Time) Priority {
	if travel != nil && travel.Before(now.Add(urgentTravelHours*time.Hour)) && !travel.Before(now.Add(-24*time.Hour)) {
		return PriorityHigh
	}
	return PriorityMedium
}

// Normalize canonicalises the profile and validates it against now; an empty
// priority is derived from travel. A follow-up equal to stored (the saved value)
// is kept even when it has passed. Field problems come back together.
func (p Profile) Normalize(now time.Time, travel, stored *time.Time) (Profile, error) {
	p.Email = shared.NormalizeEmail(p.Email)
	p.Segment = Segment(strings.ToLower(strings.TrimSpace(string(p.Segment))))
	p.CompanyName = strings.TrimSpace(p.CompanyName)
	p.TaxNumber = strings.ToUpper(strings.Join(strings.Fields(p.TaxNumber), ""))
	p.TaxOffice = strings.TrimSpace(p.TaxOffice)
	p.Priority = Priority(strings.ToLower(strings.TrimSpace(string(p.Priority))))
	p.Intent = Intent(strings.ToLower(strings.TrimSpace(string(p.Intent))))
	if p.Segment == "" {
		p.Segment = SegmentB2C
	}
	if p.Priority == "" {
		p.Priority = SuggestedPriority(travel, now)
	}

	fields := map[string]any{}
	if p.Email != "" {
		addr, err := mail.ParseAddress(p.Email)
		if err != nil || addr.Address != p.Email || len(p.Email) > maxEmailLen {
			fields["email"] = "invalid email"
		}
	}
	switch p.Segment {
	case SegmentB2C:
		p.CompanyName, p.TaxNumber, p.TaxOffice = "", "", ""
	case SegmentB2B:
		if p.CompanyName == "" {
			fields["company_name"] = "required for corporate leads"
		}
		if utf8.RuneCountInString(p.CompanyName) > maxCompanyLen {
			fields["company_name"] = "too long"
		}
		if p.TaxNumber != "" && !taxNumber.MatchString(p.TaxNumber) {
			fields["tax_number"] = "must be 4-20 letters or digits"
		}
		if utf8.RuneCountInString(p.TaxOffice) > maxTaxOfficeLen {
			fields["tax_office"] = "too long"
		}
	default:
		fields["segment"] = "must be b2c or b2b"
	}
	if !ValidPriority(p.Priority) {
		fields["priority"] = "must be high, medium or low"
	}
	switch p.Intent {
	case "", IntentReady, IntentComparing, IntentPlanning:
	default:
		fields["intent"] = "must be ready, comparing or planning"
	}
	if p.NextFollowUpAt != nil {
		at := p.NextFollowUpAt.UTC().Truncate(time.Minute)
		unchanged := stored != nil && stored.UTC().Truncate(time.Minute).Equal(at)
		if !unchanged && (at.Before(now.Add(-followUpPastSkew)) || at.After(now.Add(maxFollowUpAhead))) {
			fields["next_follow_up_at"] = "must be within the next year"
		}
		p.NextFollowUpAt = &at
	}
	if len(fields) > 0 {
		err := shared.NewValidation("invalid lead profile")
		err.Details = fields
		return p, err
	}
	return p, nil
}
