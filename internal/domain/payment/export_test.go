package payment

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBuildQueueCSV(t *testing.T) {
	out, err := BuildQueueCSV([]QueueItem{{
		Kind: QueueCredit, BookingID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		BookingRef: "11111111", CustomerName: "=HYPERLINK(\"x\")", Amount: -500, Currency: "SAR",
		Status: "credit", Note: "@sum, \"quoted\"",
	}})
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.HasPrefix(s, "\ufeffkind,booking_id,") {
		t.Fatalf("missing BOM/header: %q", s[:20])
	}
	for _, want := range []string{`"'=HYPERLINK(""x"")"`, ",-500,", `"'@sum, ""quoted"""`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing neutralized cell %s in %q", want, s)
		}
	}
}
