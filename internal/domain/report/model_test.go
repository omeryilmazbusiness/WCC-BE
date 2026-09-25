package report

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeDefaults(t *testing.T) {
	f, err := Filter{BranchID: uuid.New()}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if !f.To.After(f.From) || f.Limit != 200 {
		t.Fatalf("%#v", f)
	}
}

func TestNormalizeRejectsBadPeriod(t *testing.T) {
	now := time.Now().UTC()
	_, err := Filter{BranchID: uuid.New(), From: now, To: now}.Normalize()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildCSVAndSensitive(t *testing.T) {
	if !IsSensitive(KindFinance) || IsSensitive(KindSLA) {
		t.Fatal("sensitive matrix")
	}
	cols := ColumnsFor(KindSales)
	rows := []Row{{
		ID: "1", Label: "Alice",
		Metrics: map[string]any{
			"leads_handled": 3, "leads_won": 1, "conversion_bps": 3333,
			"open_tasks": 2, "overdue_tasks": 0, "collected_amt": int64(1000),
		},
		Severity:   "info",
		Drilldowns: []DrillRef{{HrefHint: "/pipeline"}},
	}}
	csv, err := BuildCSV(cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	s := string(csv)
	if !strings.Contains(s, "Alice") || !strings.Contains(s, "leads_handled") || !strings.HasPrefix(s, "\ufeff") {
		t.Fatalf("csv=%q", s[:min(120, len(s))])
	}
}

func TestValidKind(t *testing.T) {
	if !ValidKind(KindSales) || ValidKind("nope") {
		t.Fatal("kind")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
