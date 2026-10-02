// Package company models tenants: a company owns one or more branches, every
// user works in exactly one branch, and a GM acts across the company.
package company

import (
	"context"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	DefaultCurrency = "USD"
	DefaultTimezone = "Asia/Riyadh"

	MainCenterNameEN = "Main Center"
	MainCenterNameAR = "المركز الرئيسي"
	MainCenterSlug   = "main"
)

var (
	slugPattern     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	codePattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{1,15}$`)
	phonePattern    = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{5,19}$`)
	countryPattern  = regexp.MustCompile(`^[A-Z]{2}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

// reservedSlugs are first path segments the web app owns; a company slug
// must never shadow them in /{locale}/{company}/{branch}/... URLs.
var reservedSlugs = map[string]struct{}{
	"api": {}, "app": {}, "admin": {}, "auth": {}, "login": {}, "logout": {}, "setup": {},
	"manager": {}, "workspace": {}, "pipeline": {}, "inbox": {}, "tasks": {}, "notifications": {},
	"customers": {}, "packages": {}, "bookings": {}, "finance": {}, "targets": {}, "reports": {},
	"suppliers": {}, "rooming": {}, "integrations": {}, "security": {}, "settings": {},
	"import-export": {}, "missing-docs": {}, "flights": {}, "static": {}, "public": {}, "assets": {},
	"en": {}, "ar": {}, "www": {}, "help": {}, "support": {}, "status": {},
}

// reservedBranchSlugs would shadow company pages in /{locale}/{company}/{segment}
// URLs, such as the company sign-in page.
var reservedBranchSlugs = map[string]struct{}{"login": {}}

var latinFold = strings.NewReplacer(
	"ç", "c", "ğ", "g", "ı", "i", "\u0307", "", "ö", "o", "ş", "s", "ü", "u",
	"à", "a", "á", "a", "â", "a", "ä", "a", "ã", "a", "å", "a", "è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ò", "o", "ó", "o", "ô", "o", "õ", "o", "ù", "u", "ú", "u",
	"û", "u", "ñ", "n", "ß", "ss",
)

// Slugify turns a display name into a URL segment ("Al Noor Travel" → "al-noor-travel").
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range latinFold.Replace(strings.ToLower(name)) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		default:
			if b.Len() > 0 && !dash {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if len(s) > 48 {
		s = strings.TrimRight(s[:48], "-")
	}
	return s
}

func validSlug(s string) bool {
	return len(s) >= 2 && len(s) <= 48 && slugPattern.MatchString(s)
}

// ValidCompanySlug also rejects slugs reserved by the web app routes.
func ValidCompanySlug(s string) bool {
	_, reserved := reservedSlugs[s]
	return validSlug(s) && !reserved
}

// Company is a tenant and its public profile.
type Company struct {
	ID        uuid.UUID
	Slug      string
	NameEN    string
	NameAR    string
	LegalName string
	Phone     string
	Email     string
	Website   string
	Country   string
	City      string
	Address   string
	Currency  string
	Timezone  string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProfileLevel selects how strict Normalize is: registration needs a name,
// the GM setup also needs where the company is.
type ProfileLevel int

const (
	ProfileBasic ProfileLevel = iota
	ProfileComplete
)

// Normalize trims, fills defaults (slug from the English name, currency,
// timezone) and validates. Field problems are reported together in
// Details, keyed by JSON field name.
func (c *Company) Normalize(level ProfileLevel) error {
	c.Slug = strings.ToLower(strings.TrimSpace(c.Slug))
	c.NameEN = strings.TrimSpace(c.NameEN)
	c.NameAR = strings.TrimSpace(c.NameAR)
	c.LegalName = strings.TrimSpace(c.LegalName)
	c.Phone = strings.TrimSpace(c.Phone)
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	c.Website = strings.TrimSpace(c.Website)
	c.Country = strings.ToUpper(strings.TrimSpace(c.Country))
	c.City = strings.TrimSpace(c.City)
	c.Address = strings.TrimSpace(c.Address)
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	c.Timezone = strings.TrimSpace(c.Timezone)
	if c.Slug == "" {
		c.Slug = Slugify(c.NameEN)
	}
	if c.Currency == "" {
		c.Currency = DefaultCurrency
	}
	if c.Timezone == "" {
		c.Timezone = DefaultTimezone
	}

	fields := map[string]any{}
	if n := utf8.RuneCountInString(c.NameEN); n < 2 || n > 120 {
		fields["name_en"] = "2-120 characters"
	}
	if !ValidCompanySlug(c.Slug) {
		fields["slug"] = "2-48 lowercase letters, digits or dashes; not a reserved word"
	}
	if utf8.RuneCountInString(c.NameAR) > 120 {
		fields["name_ar"] = "at most 120 characters"
	}
	if utf8.RuneCountInString(c.LegalName) > 200 {
		fields["legal_name"] = "at most 200 characters"
	}
	if c.Phone != "" && !phonePattern.MatchString(c.Phone) {
		fields["phone"] = "invalid phone number"
	}
	if c.Email != "" {
		if a, err := mail.ParseAddress(c.Email); err != nil || a.Address != c.Email {
			fields["email"] = "invalid email"
		}
	}
	if c.Website != "" {
		site, ok := normalizeWebsite(c.Website)
		if !ok {
			fields["website"] = "invalid website"
		}
		c.Website = site
	}
	if c.Country != "" && !countryPattern.MatchString(c.Country) {
		fields["country"] = "ISO 3166 alpha-2 code"
	}
	if utf8.RuneCountInString(c.City) > 80 {
		fields["city"] = "at most 80 characters"
	}
	if utf8.RuneCountInString(c.Address) > 300 {
		fields["address"] = "at most 300 characters"
	}
	if level == ProfileComplete {
		for key, v := range map[string]string{"country": c.Country, "city": c.City, "address": c.Address} {
			if v == "" {
				fields[key] = "required"
			}
		}
	}
	if !currencyPattern.MatchString(c.Currency) {
		fields["currency"] = "ISO 4217 code"
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		fields["timezone"] = "unknown timezone"
	}
	return fieldErrors("invalid company profile", fields)
}

func normalizeWebsite(raw string) (string, bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.Contains(u.Host, ".") {
		return raw, false
	}
	return u.String(), true
}

func fieldErrors(msg string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	err := shared.NewValidation(msg)
	err.Details = fields
	return err
}

// Kind tells the headquarters apart from ordinary branches.
type Kind string

const (
	KindMainCenter Kind = "main_center"
	KindBranch     Kind = "branch"
)

func ParseKind(s string) (Kind, bool) {
	switch Kind(s) {
	case KindMainCenter, KindBranch:
		return Kind(s), true
	}
	return "", false
}

// Branch is a location of a company. Its slug is unique within the company
// and forms the second URL segment.
type Branch struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	Code      string
	Slug      string
	NameEN    string
	NameAR    string
	Kind      Kind
	Timezone  string
	IsActive  bool
	CreatedAt time.Time
}

// NewMainCenter is the default branch every company starts with.
func NewMainCenter(c *Company) Branch {
	return Branch{
		ID: uuid.New(), CompanyID: c.ID, Code: DefaultCode(c.Slug),
		Slug: MainCenterSlug, NameEN: MainCenterNameEN, NameAR: MainCenterNameAR,
		Kind: KindMainCenter, Timezone: c.Timezone, IsActive: true,
	}
}

// DefaultCode derives a branch code from a slug ("al-noor-travel" → "ALNOORTRAV").
func DefaultCode(slug string) string {
	code := strings.ToUpper(strings.ReplaceAll(slug, "-", ""))
	if len(code) > 10 {
		code = code[:10]
	}
	if len(code) < 2 {
		code = "BR" + code
	}
	return code
}

// BranchCode builds a readable default code for a company's extra branch:
// a short company prefix plus the branch name ("al-noor", "jeddah" →
// "ALNOJEDDAH").
func BranchCode(companySlug, branchSlug string) string {
	prefix := strings.ToUpper(strings.ReplaceAll(companySlug, "-", ""))
	if len(prefix) > 4 {
		prefix = prefix[:4]
	}
	return DefaultCode(prefix + branchSlug)
}

// Normalize trims, derives the slug from the English name when empty and
// validates.
func (b *Branch) Normalize() error {
	b.Code = strings.TrimSpace(b.Code)
	b.NameEN = strings.TrimSpace(b.NameEN)
	b.NameAR = strings.TrimSpace(b.NameAR)
	b.Slug = strings.ToLower(strings.TrimSpace(b.Slug))
	b.Timezone = strings.TrimSpace(b.Timezone)
	if b.Slug == "" {
		b.Slug = Slugify(b.NameEN)
	}
	if b.Kind == "" {
		b.Kind = KindBranch
	}
	if b.Timezone == "" {
		b.Timezone = DefaultTimezone
	}
	fields := map[string]any{}
	if n := utf8.RuneCountInString(b.NameEN); n < 2 || n > 80 {
		fields["name_en"] = "2-80 characters"
	}
	if utf8.RuneCountInString(b.NameAR) > 80 {
		fields["name_ar"] = "at most 80 characters"
	}
	if !validSlug(b.Slug) {
		fields["slug"] = "2-48 lowercase letters, digits or dashes"
	} else if _, reserved := reservedBranchSlugs[b.Slug]; reserved {
		fields["slug"] = "reserved word"
	}
	if !codePattern.MatchString(b.Code) {
		fields["code"] = "2-16 letters, digits, dash or underscore"
	}
	if _, ok := ParseKind(string(b.Kind)); !ok {
		fields["kind"] = "main_center or branch"
	}
	if _, err := time.LoadLocation(b.Timezone); err != nil {
		fields["timezone"] = "unknown timezone"
	}
	return fieldErrors("invalid branch", fields)
}

// Workspace is the tenant context of a signed-in user: their company and
// every branch in it (the home branch included).
type Workspace struct {
	Company  Company
	Branches []Branch
}

// BranchIDs lists every branch of the company.
func (w *Workspace) BranchIDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(w.Branches))
	for _, b := range w.Branches {
		out = append(out, b.ID)
	}
	return out
}

// Branch finds a branch of the company.
func (w *Workspace) Branch(id uuid.UUID) (Branch, bool) {
	for _, b := range w.Branches {
		if b.ID == id {
			return b, true
		}
	}
	return Branch{}, false
}

// Page is a slice of companies for the platform console.
type Page struct {
	Items []Summary
	Total int
}

// Summary is a company row with its footprint.
type Summary struct {
	Company
	BranchCount int
	UserCount   int
	GMEmail     string
	// LogoUpdatedAt is nil when the company has no logo.
	LogoUpdatedAt *time.Time
}

// Repository persists companies and branches (DIP).
type Repository interface {
	CreateCompany(ctx context.Context, c *Company) error
	GetCompany(ctx context.Context, id uuid.UUID) (*Company, error)
	// UpdateCompany saves the profile; branches still on the old company
	// timezone follow a timezone change.
	UpdateCompany(ctx context.Context, c *Company, actor uuid.UUID) error
	ListCompanies(ctx context.Context, query string, limit, offset int) (Page, error)

	CreateBranch(ctx context.Context, b *Branch) error
	UpdateBranch(ctx context.Context, b *Branch) error
	// DemoteMainCenter turns the current main center (if any, other than
	// keep) into an ordinary branch.
	DemoteMainCenter(ctx context.Context, companyID, keep uuid.UUID) error
	ListBranches(ctx context.Context, companyID uuid.UUID) ([]Branch, error)

	// WorkspaceOf resolves the company of a branch with all its branches.
	WorkspaceOf(ctx context.Context, branchID uuid.UUID) (*Workspace, error)
	// LockCompany serializes branch changes of a company until the tx ends.
	LockCompany(ctx context.Context, companyID uuid.UUID) error
}
