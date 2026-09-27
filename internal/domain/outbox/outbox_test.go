package outbox

import (
	"strings"
	"testing"
	"time"
)

func TestBackoffDoublesAndCaps(t *testing.T) {
	cases := map[int]time.Duration{0: 5 * time.Second, 1: 5 * time.Second, 2: 10 * time.Second, 3: 20 * time.Second, 10: 30 * time.Minute, 50: 30 * time.Minute}
	for attempt, want := range cases {
		if got := Backoff(attempt); got != want {
			t.Errorf("Backoff(%d)=%s want %s", attempt, got, want)
		}
	}
}

func TestAfterFailureDeadLettersAtMax(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	next, dead := AfterFailure(1, now)
	if dead || !next.Equal(now.Add(5*time.Second)) {
		t.Fatalf("first failure: next=%s dead=%v", next, dead)
	}
	if _, dead := AfterFailure(MaxAttempts-1, now); dead {
		t.Fatal("dead-lettered one attempt early")
	}
	if _, dead := AfterFailure(MaxAttempts, now); !dead {
		t.Fatal("not dead-lettered at MaxAttempts")
	}
}

func TestTruncateError(t *testing.T) {
	if got := TruncateError(strings.Repeat("x", 5000)); len(got) != 2000 {
		t.Fatalf("len=%d", len(got))
	}
	if TruncateError("short") != "short" {
		t.Fatal("short errors must be kept")
	}
}
