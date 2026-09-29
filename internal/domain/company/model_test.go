package company

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func details(t *testing.T, err error) map[string]any {
	t.Helper()
	var app *shared.AppError
	if !errors.As(err, &app) {
		t.Fatalf("expected AppError, got %v", err)
	}
	return app.Details
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Al Noor Travel":        "al-noor-travel",
		"  Wifad  ":             "wifad",
		"İstanbul Şube – Çarşı": "istanbul-sube-carsi",
		"Main Center":           "main-center",
		"النور":                 "",
		"A&B__Tours!!":          "a-b-tours",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
	long := Slugify("a very long company name that keeps going and going beyond limits")
	if len(long) > 48 || long[len(long)-1] == '-' {
		t.Fatalf("long slug = %q", long)
	}
}

func TestCompanySlugRules(t *testing.T) {
	for _, s := range []string{"wifad", "al-noor", "a1"} {
		if !ValidCompanySlug(s) {
			t.Errorf("%q must be valid", s)
		}
	}
	for _, s := range []string{"", "a", "-x", "x-", "a--b", "Upper", "setup", "api", "manager", "en"} {
		if ValidCompanySlug(s) {
			t.Errorf("%q must be rejected", s)
		}
	}
}

func TestCompanyNormalizeBasic(t *testing.T) {
	c := Company{NameEN: " Al Noor Travel ", Email: " Info@AlNoor.com ", Website: "alnoor.com", Country: "sa", Currency: "sar"}
	if err := c.Normalize(ProfileBasic); err != nil {
		t.Fatal(err)
	}
	if c.Slug != "al-noor-travel" || c.Email != "info@alnoor.com" || c.Website != "https://alnoor.com" ||
		c.Country != "SA" || c.Currency != "SAR" || c.Timezone != DefaultTimezone {
		t.Fatalf("normalized = %+v", c)
	}
}

func TestCompanyNormalizeCompleteNeedsLocation(t *testing.T) {
	c := Company{NameEN: "Wifad"}
	fields := details(t, c.Normalize(ProfileComplete))
	for _, key := range []string{"country", "city", "address"} {
		if fields[key] != "required" {
			t.Errorf("%s: %v", key, fields[key])
		}
	}
	c = Company{NameEN: "Wifad", Country: "TR", City: "Istanbul", Address: "Levent 1"}
	if err := c.Normalize(ProfileComplete); err != nil {
		t.Fatal(err)
	}
}

func TestCompanyNormalizeFieldErrors(t *testing.T) {
	c := Company{NameEN: "x", Slug: "setup", Email: "bad", Phone: "12", Country: "TUR", Currency: "dollar", Timezone: "Mars/Base", Website: "nope"}
	fields := details(t, c.Normalize(ProfileBasic))
	for _, key := range []string{"name_en", "slug", "email", "phone", "country", "currency", "timezone", "website"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing %s in %v", key, fields)
		}
	}
}

func TestBranchNormalize(t *testing.T) {
	b := Branch{NameEN: "Jeddah Office", Code: "JED"}
	if err := b.Normalize(); err != nil {
		t.Fatal(err)
	}
	if b.Slug != "jeddah-office" || b.Kind != KindBranch || b.Timezone != DefaultTimezone {
		t.Fatalf("branch = %+v", b)
	}
	bad := Branch{NameEN: "J", Code: "!", Kind: "hq", Timezone: "nowhere"}
	fields := details(t, bad.Normalize())
	for _, key := range []string{"name_en", "code", "kind", "timezone", "slug"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("missing %s in %v", key, fields)
		}
	}
}

func TestMainCenterDefaults(t *testing.T) {
	c := &Company{ID: uuid.New(), Slug: "al-noor-travel-agency", Timezone: "Europe/Istanbul"}
	b := NewMainCenter(c)
	if b.Kind != KindMainCenter || b.Slug != MainCenterSlug || b.NameEN != MainCenterNameEN ||
		b.CompanyID != c.ID || b.Timezone != c.Timezone || b.Code != "ALNOORTRAV" {
		t.Fatalf("main center = %+v", b)
	}
	if err := b.Normalize(); err != nil {
		t.Fatal(err)
	}
	if DefaultCode("x") != "BRX" {
		t.Fatalf("short code = %q", DefaultCode("x"))
	}
	if got := BranchCode("al-noor-travel", "jeddah-office"); got != "ALNOJEDDAH" {
		t.Fatalf("branch code = %q", got)
	}
	if BranchCode("al-noor-travel", "jeddah") == DefaultCode("al-noor-travel") {
		t.Fatal("a branch code must not start as the main center code")
	}
}

func TestWorkspaceLookup(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	w := Workspace{Branches: []Branch{{ID: a}, {ID: b}}}
	if ids := w.BranchIDs(); len(ids) != 2 || ids[0] != a {
		t.Fatalf("ids = %v", ids)
	}
	if _, ok := w.Branch(b); !ok {
		t.Fatal("branch b must be found")
	}
	if _, ok := w.Branch(uuid.New()); ok {
		t.Fatal("foreign branch must not be found")
	}
}
