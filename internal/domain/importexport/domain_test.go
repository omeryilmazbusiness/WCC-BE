package importexport_test

import (
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/importexport"
)

func TestSuggestMapping(t *testing.T) {
	m := importexport.SuggestMapping([]string{"Mobile", "Full Name", "E-Mail", "DOB", "Currency"})
	if m["phone"] != "Mobile" {
		t.Fatalf("phone: got %q", m["phone"])
	}
	if m["full_name"] != "Full Name" {
		t.Fatalf("full_name: got %q", m["full_name"])
	}
	if m["email"] != "E-Mail" {
		t.Fatalf("email: got %q", m["email"])
	}
	if m["date_of_birth"] != "DOB" {
		t.Fatalf("dob: got %q", m["date_of_birth"])
	}
	if m["currency"] != "Currency" {
		t.Fatalf("currency: got %q", m["currency"])
	}
}

func TestNormalizePhoneDateCurrencyStatus(t *testing.T) {
	if got := importexport.NormalizePhone("+966 50-123-4567"); got != "+966501234567" {
		t.Fatalf("phone: %q", got)
	}
	if got := importexport.NormalizeCurrency(" aed "); got != "AED" {
		t.Fatalf("currency: %q", got)
	}
	if got := importexport.NormalizeStatus("On Track"); got != "on_track" {
		t.Fatalf("status: %q", got)
	}

	d, err := importexport.NormalizeDate("2024-03-15")
	if err != nil || d == nil || d.Format("2006-01-02") != "2024-03-15" {
		t.Fatalf("iso date: %v %v", d, err)
	}
	d, err = importexport.NormalizeDate("15/03/2024")
	if err != nil || d == nil || d.Format("2006-01-02") != "2024-03-15" {
		t.Fatalf("dmy date: %v %v", d, err)
	}
	// Excel serial for 2024-03-15 ≈ 45366
	d, err = importexport.NormalizeDate("45366")
	if err != nil || d == nil {
		t.Fatalf("excel serial: %v", err)
	}
	if d.Year() != 2024 || d.Month() != time.March || d.Day() != 15 {
		t.Fatalf("excel serial day: %v", d)
	}
}

func TestParseCSV(t *testing.T) {
	raw := []byte("Name,Phone\nAlice,+971501112233\nBob,+971502223344\n")
	sheet, err := importexport.ParseCSV(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(sheet.Headers) != 2 || sheet.Headers[0] != "Name" {
		t.Fatalf("headers: %#v", sheet.Headers)
	}
	if len(sheet.Rows) != 2 {
		t.Fatalf("rows: %d", len(sheet.Rows))
	}
	m := importexport.SuggestMapping(sheet.Headers)
	vals := importexport.MapRow(sheet.Headers, sheet.Rows[0], m)
	if vals["full_name"] != "Alice" || importexport.NormalizePhone(vals["phone"]) != "+971501112233" {
		t.Fatalf("mapped: %#v", vals)
	}
}
