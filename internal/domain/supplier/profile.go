package supplier

import (
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Supplier categories.
const (
	CategoryGDS        = "gds"
	CategoryWholesaler = "wholesaler"
	CategoryDMC        = "dmc"
	CategoryTransfer   = "transfer"
	CategoryVisa       = "visa"
	CategoryInsurance  = "insurance"
	CategoryOther      = "other"
)

var Categories = []string{CategoryGDS, CategoryWholesaler, CategoryDMC, CategoryTransfer, CategoryVisa, CategoryInsurance, CategoryOther}

// Integration types.
const (
	IntegrationAPI    = "api"
	IntegrationFeed   = "feed"
	IntegrationManual = "manual"
)

var IntegrationTypes = []string{IntegrationAPI, IntegrationFeed, IntegrationManual}

// Environments.
const (
	EnvSandbox    = "sandbox"
	EnvProduction = "production"
)

// Credential keys kept sealed; anything else is rejected.
const (
	CredAPIKey       = "api_key"
	CredClientID     = "client_id"
	CredClientSecret = "client_secret"
	CredAccountID    = "account_id"
)

var CredentialKeys = []string{CredAPIKey, CredClientID, CredClientSecret, CredAccountID}

// Coverage regions.
var Regions = []string{"makkah", "madinah", "jeddah", "riyadh", "saudi", "gcc", "middle_east", "turkey", "europe", "asia", "global"}

const (
	MaxNameLen        = 160
	MaxTermsLen       = 4000
	MaxURLLen         = 500
	MaxFreeCancelHrs  = 8760
	MaxCredentialLen  = 4096
	MaxMarkupBps      = 50_000
	MaxMoney          = 1_000_000_000_000
	ContractWarnDays  = 30
	maxEmergencyPhone = 24
)

var (
	codePattern     = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{1,39}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	phonePattern    = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{5,19}$`)
)

// Integration is how the supplier is connected. Credentials are sealed and
// never part of this struct.
type Integration struct {
	Type        string
	Environment string
	BaseURL     string
	WebhookURL  string
}

// Markups are default margins in basis points per product (300 = 3%).
type Markups map[string]int

// Products a supplier markup can target.
const (
	ProductFlight    = "flight"
	ProductHotel     = "hotel"
	ProductTransfer  = "transfer"
	ProductVisa      = "visa"
	ProductInsurance = "insurance"
	ProductPackage   = "package"
)

var Products = []string{ProductFlight, ProductHotel, ProductTransfer, ProductVisa, ProductInsurance, ProductPackage}

func (m Markups) normalize() (Markups, string) {
	out := Markups{}
	for k, v := range m {
		k = strings.ToLower(strings.TrimSpace(k))
		if !slices.Contains(Products, k) {
			return nil, "unknown product " + k
		}
		if v < 0 || v > MaxMarkupBps {
			return nil, "markup must be 0-500%"
		}
		if v > 0 {
			out[k] = v
		}
	}
	return out, ""
}

// Normalize trims and validates the profile; problems are keyed by API field.
func (s *Supplier) Normalize() error {
	f := fields{}
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	s.NameEn = strings.TrimSpace(s.NameEn)
	s.NameAr = strings.TrimSpace(s.NameAr)
	s.ContactName = strings.TrimSpace(s.ContactName)
	s.ContactPhone = strings.TrimSpace(s.ContactPhone)
	s.ContactEmail = strings.TrimSpace(s.ContactEmail)
	s.EmergencyPhone = strings.TrimSpace(s.EmergencyPhone)
	s.Terms = strings.TrimSpace(s.Terms)
	s.Category = strings.ToLower(strings.TrimSpace(s.Category))
	if s.Category == "" {
		s.Category = CategoryOther
	}
	if !codePattern.MatchString(s.Code) {
		f.add("code", "2-40 letters, digits or dashes, e.g. SUP-DUFFEL-01")
	}
	if s.NameEn == "" && s.NameAr == "" {
		f.add("name_en", "name is required")
	}
	if utf8.RuneCountInString(s.NameEn) > MaxNameLen {
		f.add("name_en", "at most 160 characters")
	}
	if utf8.RuneCountInString(s.NameAr) > MaxNameLen {
		f.add("name_ar", "at most 160 characters")
	}
	if !slices.Contains(Categories, s.Category) {
		f.add("category", "unknown category")
	}
	if utf8.RuneCountInString(s.ContactName) > MaxNameLen {
		f.add("contact_name", "at most 160 characters")
	}
	if s.ContactPhone != "" && !phonePattern.MatchString(s.ContactPhone) {
		f.add("contact_phone", "invalid phone number")
	}
	if s.EmergencyPhone != "" && (!phonePattern.MatchString(s.EmergencyPhone) || len(s.EmergencyPhone) > maxEmergencyPhone) {
		f.add("emergency_phone", "invalid phone number")
	}
	if s.ContactEmail != "" && !validEmail(s.ContactEmail) {
		f.add("contact_email", "invalid email")
	}
	if utf8.RuneCountInString(s.Terms) > MaxTermsLen {
		f.add("terms", "at most 4000 characters")
	}
	s.normalizeIntegration(f)
	s.Finance.normalize(f)
	markups, msg := s.Markups.normalize()
	if msg != "" {
		f.add("markups", msg)
	}
	s.Markups = markups
	s.Regions = normalizeSet(s.Regions, Regions)
	if s.FreeCancelHours < 0 || s.FreeCancelHours > MaxFreeCancelHrs {
		f.add("free_cancel_hours", "0-8760 hours")
	}
	if s.ContractStart != nil && s.ContractEnd != nil && s.ContractEnd.Before(*s.ContractStart) {
		f.add("contract_end", "must be on or after the start date")
	}
	return f.err("invalid supplier")
}

func (s *Supplier) normalizeIntegration(f fields) {
	in := &s.Integration
	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	in.Environment = strings.ToLower(strings.TrimSpace(in.Environment))
	in.BaseURL = strings.TrimSpace(in.BaseURL)
	in.WebhookURL = strings.TrimSpace(in.WebhookURL)
	if in.Type == "" {
		in.Type = IntegrationManual
	}
	if in.Environment == "" {
		in.Environment = EnvSandbox
	}
	if !slices.Contains(IntegrationTypes, in.Type) {
		f.add("integration_type", "api, feed or manual")
	}
	if in.Environment != EnvSandbox && in.Environment != EnvProduction {
		f.add("environment", "sandbox or production")
	}
	if in.BaseURL != "" && !ValidHTTPSURL(in.BaseURL) {
		f.add("api_base_url", "an https:// URL")
	}
	if in.WebhookURL != "" && !ValidHTTPSURL(in.WebhookURL) {
		f.add("webhook_url", "an https:// URL")
	}
}

// ValidHTTPSURL accepts absolute https URLs with a host and no credentials.
func ValidHTTPSURL(raw string) bool {
	if len(raw) > MaxURLLen {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

// NormalizeCredentials validates incoming credential changes: only known
// keys, bounded length; empty values mean "clear".
func NormalizeCredentials(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	f := fields{}
	for k, v := range in {
		k = strings.ToLower(strings.TrimSpace(k))
		if !slices.Contains(CredentialKeys, k) {
			f.add("credentials", "unknown credential "+k)
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) > MaxCredentialLen {
			f.add("credentials", k+" is too long")
			continue
		}
		out[k] = v
	}
	if err := f.err("invalid credentials"); err != nil {
		return nil, err
	}
	return out, nil
}

// ContractDaysLeft is the number of days until the contract ends (negative
// once expired); ok is false without an end date.
func (s *Supplier) ContractDaysLeft(today time.Time) (int, bool) {
	if s.ContractEnd == nil {
		return 0, false
	}
	return int(Day(*s.ContractEnd, time.UTC).Sub(Day(today, time.UTC)).Hours() / 24), true
}

// Day truncates t to its calendar day in loc, returned as UTC midnight.
func Day(t time.Time, loc *time.Location) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// ParseDay parses YYYY-MM-DD.
func ParseDay(v string) (time.Time, error) {
	return time.Parse(time.DateOnly, strings.TrimSpace(v))
}

func FormatDay(t time.Time) string { return t.UTC().Format(time.DateOnly) }

func validEmail(v string) bool {
	if len(v) > 254 {
		return false
	}
	a, err := mail.ParseAddress(v)
	return err == nil && a.Address == v && strings.Contains(v[strings.LastIndex(v, "@")+1:], ".")
}

func normalizeSet(in []string, allowed []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range allowed {
		for _, x := range in {
			if strings.ToLower(strings.TrimSpace(x)) == v {
				out = append(out, v)
				break
			}
		}
	}
	return out
}

type fields map[string]any

func (f fields) add(key, msg string) {
	if _, ok := f[key]; !ok {
		f[key] = msg
	}
}

func (f fields) err(msg string) error {
	if len(f) == 0 {
		return nil
	}
	e := shared.NewValidation(msg)
	e.Details = map[string]any(f)
	return e
}
