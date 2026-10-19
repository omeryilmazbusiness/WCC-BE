package revenuetarget

import "testing"

func TestNormalizeTargetCustomers(t *testing.T) {
	if got, err := NormalizeTargetCustomers(nil); err != nil || got != nil {
		t.Fatalf("nil: got %v err %v", got, err)
	}
	ok := 120
	got, err := NormalizeTargetCustomers(&ok)
	if err != nil || got == nil || *got != 120 {
		t.Fatalf("ok: got %v err %v", got, err)
	}
	for _, n := range []int{0, -1, MaxTargetCustomers + 1} {
		if _, err := NormalizeTargetCustomers(&n); err == nil {
			t.Fatalf("expected error for %d", n)
		}
	}
}

func TestCombineStatus(t *testing.T) {
	cases := []struct {
		money, customers, want Status
	}{
		{StatusAhead, StatusAhead, StatusAhead},
		{StatusAhead, StatusOnTrack, StatusOnTrack},
		{StatusOnTrack, StatusBehind, StatusBehind},
		{StatusBehind, StatusAhead, StatusBehind},
		{StatusPlaceholder, StatusOnTrack, StatusOnTrack},
	}
	for _, c := range cases {
		if got := CombineStatus(c.money, c.customers); got != c.want {
			t.Errorf("CombineStatus(%s,%s)=%s want %s", c.money, c.customers, got, c.want)
		}
	}
}
