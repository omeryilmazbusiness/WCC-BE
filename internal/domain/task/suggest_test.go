package task

import (
	"testing"
	"time"
)

func TestSuggestNextTask(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		outcome CallOutcome
		wantOK  bool
		kind    string
	}{
		{OutcomeFollowUp, true, "follow_up"},
		{OutcomeNoAnswer, true, "follow_up"},
		{OutcomeSendQuote, true, "quote"},
		{OutcomeDocsPending, true, "document"},
		{OutcomePaymentDue, true, "payment"},
		{OutcomeClosedWon, false, ""},
		{OutcomeClosedLost, false, ""},
		{CallOutcome("unknown_x"), true, "follow_up"},
	}
	for _, tc := range cases {
		sug, ok := SuggestNextTask(tc.outcome, now)
		if ok != tc.wantOK {
			t.Fatalf("%s ok=%v want %v", tc.outcome, ok, tc.wantOK)
		}
		if !tc.wantOK {
			continue
		}
		if sug.Kind != tc.kind {
			t.Fatalf("%s kind=%q want %q", tc.outcome, sug.Kind, tc.kind)
		}
		if sug.Title == "" || sug.DueIn <= 0 {
			t.Fatalf("%s incomplete %#v", tc.outcome, sug)
		}
		due := SuggestedDueAt(sug, now)
		if !due.Equal(now.Add(sug.DueIn)) {
			t.Fatalf("due mismatch %v", due)
		}
	}
}
