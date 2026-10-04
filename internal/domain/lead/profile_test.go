package lead

import (
	"errors"
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestProfileNormalize(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	got, err := Profile{
		Email:          " Ayse@Example.COM ",
		Segment:        " B2B ",
		CompanyName:    " Acme Travel ",
		TaxNumber:      "123 456 7890",
		TaxOffice:      " Kadıköy ",
		Intent:         "Ready",
		NextFollowUpAt: ptr(now.Add(3*time.Hour + 42*time.Second)),
	}.Normalize(now, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "ayse@example.com" || got.Segment != SegmentB2B || got.CompanyName != "Acme Travel" {
		t.Fatalf("got %+v", got)
	}
	if got.TaxNumber != "1234567890" || got.TaxOffice != "Kadıköy" || got.Intent != IntentReady {
		t.Fatalf("got %+v", got)
	}
	if got.Priority != PriorityMedium {
		t.Fatalf("priority = %s", got.Priority)
	}
	if !got.NextFollowUpAt.Equal(now.Add(3 * time.Hour)) {
		t.Fatalf("follow-up = %v", got.NextFollowUpAt)
	}
}

func TestProfileDefaults(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	got, err := Profile{CompanyName: "dropped", TaxNumber: "X"}.Normalize(now, ptr(now.Add(20*time.Hour)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Segment != SegmentB2C || got.CompanyName != "" || got.TaxNumber != "" {
		t.Fatalf("b2c must drop corporate fields: %+v", got)
	}
	if got.Priority != PriorityHigh {
		t.Fatalf("travel within 48h must suggest high, got %s", got.Priority)
	}
	kept, err := Profile{Priority: PriorityLow}.Normalize(now, ptr(now.Add(time.Hour)), nil)
	if err != nil || kept.Priority != PriorityLow {
		t.Fatalf("explicit priority must win: %v %v", kept.Priority, err)
	}
}

func TestProfileKeepsStoredFollowUp(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	past := now.Add(-48 * time.Hour)
	if _, err := (Profile{NextFollowUpAt: &past}).Normalize(now, nil, &past); err != nil {
		t.Fatalf("unchanged past follow-up rejected: %v", err)
	}
	if _, err := (Profile{NextFollowUpAt: &past}).Normalize(now, nil, nil); err == nil {
		t.Fatal("new past follow-up accepted")
	}
}

func TestProfileRejects(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	cases := map[string]Profile{
		"email":             {Email: "not-an-email"},
		"segment":           {Segment: "b2g"},
		"company_name":      {Segment: SegmentB2B},
		"tax_number":        {Segment: SegmentB2B, CompanyName: "Acme", TaxNumber: "12"},
		"priority":          {Priority: "urgent"},
		"intent":            {Intent: "maybe"},
		"next_follow_up_at": {NextFollowUpAt: ptr(now.AddDate(2, 0, 0))},
	}
	for field, in := range cases {
		_, err := in.Normalize(now, nil, nil)
		var app *shared.AppError
		if !errors.As(err, &app) {
			t.Fatalf("%s: want validation error, got %v", field, err)
		}
		if _, ok := app.Details[field]; !ok {
			t.Fatalf("%s: details = %v", field, app.Details)
		}
	}
}
