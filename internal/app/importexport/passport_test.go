package importexport

import (
	"testing"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/importexport"
)

func TestMaskExportPassports(t *testing.T) {
	rows := []domain.ExportRow{{"passport_no": "A12345678"}, {"passport_no": ""}}
	maskExportPassports(rows)
	if rows[0]["passport_no"] != "••••5678" || rows[1]["passport_no"] != "" {
		t.Fatalf("rows = %v", rows)
	}
}

func TestImportIgnoresMaskedPassports(t *testing.T) {
	if got := importedPassport("••••5678"); got != "" {
		t.Fatalf("masked passport imported as %q", got)
	}
	if got := importedPassport(" a1234 5678 "); got != "A12345678" {
		t.Fatalf("passport = %q", got)
	}
}
