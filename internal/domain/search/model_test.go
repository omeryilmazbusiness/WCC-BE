package search

import (
	"reflect"
	"testing"
)

func TestParseKinds(t *testing.T) {
	cases := []struct {
		in   []string
		want []Kind
	}{
		{nil, nil},
		{[]string{""}, nil},
		{[]string{"booking"}, []Kind{KindBooking}},
		{[]string{" Customer , booking,customer"}, []Kind{KindCustomer, KindBooking}},
		{[]string{"lead", "passport,lead"}, []Kind{KindLead, KindPassport}},
	}
	for _, c := range cases {
		got, err := ParseKinds(c.in...)
		if err != nil {
			t.Fatalf("ParseKinds(%q) error: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("ParseKinds(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseKindsRejectsUnknown(t *testing.T) {
	if _, err := ParseKinds("customer,invoice"); err == nil {
		t.Fatal("unknown kind must be rejected")
	}
}
