package supplier

import (
	"errors"
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

var today = time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)

func day(offset int) *time.Time {
	d := today.AddDate(0, 0, offset)
	return &d
}

func validSupplier() *Supplier {
	return &Supplier{
		Code: " sup-duffel-01 ", NameEn: " Duffel Financial Ltd ", Category: "GDS",
		ContactEmail: "am@duffel.com", ContactPhone: "+44 20 1234 5678",
		Integration: Integration{Type: "API", Environment: "production", BaseURL: "https://api.duffel.com"},
		Finance:     Finance{Model: "prepaid", Currency: "sar", LowBalanceThreshold: 500_00},
		Markups:     Markups{"flight": 300, "hotel": 800, "visa": 0},
		Regions:     []string{"gcc", "GCC", "makkah", "mars"},
		IsActive:    true,
	}
}

func details(t *testing.T, err error) map[string]any {
	t.Helper()
	var ae *shared.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected app error, got %v", err)
	}
	return ae.Details
}

func TestNormalizeCleansProfile(t *testing.T) {
	s := validSupplier()
	if err := s.Normalize(); err != nil {
		t.Fatal(err)
	}
	if s.Code != "SUP-DUFFEL-01" || s.NameEn != "Duffel Financial Ltd" || s.Category != CategoryGDS {
		t.Fatalf("identity not normalized: %+v", s)
	}
	if s.Integration.Type != IntegrationAPI || s.Finance.Currency != "SAR" || s.Finance.PaymentTerms != "net30" {
		t.Fatalf("defaults not applied: %+v %+v", s.Integration, s.Finance)
	}
	if len(s.Markups) != 2 || s.Markups["visa"] != 0 {
		t.Fatalf("zero markups must be dropped: %v", s.Markups)
	}
	if len(s.Regions) != 2 {
		t.Fatalf("regions must be de-duplicated and filtered: %v", s.Regions)
	}
}

func TestNormalizeReportsFields(t *testing.T) {
	s := &Supplier{
		Code: "x", Category: "airline", ContactEmail: "nope", ContactPhone: "abc",
		Integration: Integration{Type: "soap", Environment: "staging", BaseURL: "http://api.example.com", WebhookURL: "https://u:p@h.com"},
		Finance:     Finance{Model: "barter", Currency: "dollars", CreditLimit: -1, PaymentTerms: "net90"},
		Markups:     Markups{"flight": MaxMarkupBps + 1}, FreeCancelHours: -1,
		ContractStart: day(10), ContractEnd: day(1),
	}
	f := details(t, s.Normalize())
	for _, key := range []string{
		"code", "name_en", "category", "contact_email", "contact_phone", "integration_type", "environment",
		"api_base_url", "webhook_url", "payment_model", "currency", "credit_limit", "payment_terms", "markups",
		"free_cancel_hours", "contract_end",
	} {
		if _, ok := f[key]; !ok {
			t.Errorf("missing field error %q in %v", key, f)
		}
	}
}

func TestNormalizeCredentials(t *testing.T) {
	out, err := NormalizeCredentials(map[string]string{" API_KEY ": " k-123 ", "client_secret": ""})
	if err != nil || out["api_key"] != "k-123" || out["client_secret"] != "" {
		t.Fatalf("got %v %v", out, err)
	}
	if _, err := NormalizeCredentials(map[string]string{"password": "x"}); err == nil {
		t.Fatal("unknown credential keys must be rejected")
	}
}

func TestFinanceApplyPrepaid(t *testing.T) {
	fi := Finance{Model: PaymentPrepaid, Currency: "SAR"}
	if _, err := fi.Apply(EntryCharge, 100); err == nil {
		t.Fatal("charging an empty deposit must fail")
	}
	bal, err := fi.Apply(EntryTopUp, 12_450_00)
	if err != nil || bal != 12_450_00 {
		t.Fatalf("topup = %d %v", bal, err)
	}
	if bal, _ = fi.Apply(EntryCharge, 450_00); bal != 12_000_00 {
		t.Fatalf("charge balance = %d", bal)
	}
	if _, err := fi.Apply(EntryCharge, 12_000_01); err == nil {
		t.Fatal("overdraft must fail")
	}
	if bal, _ = fi.Apply(EntryRefund, 100_00); bal != 12_100_00 {
		t.Fatalf("refund balance = %d", bal)
	}
	if _, err := fi.Apply(EntryAdjustment, -13_000_00); err == nil {
		t.Fatal("adjustment below zero must fail")
	}
	if _, err := fi.Apply(EntryPayment, 1); err == nil {
		t.Fatal("payments only apply to credit lines")
	}
	if _, err := fi.Apply(EntryTopUp, 0); err == nil {
		t.Fatal("zero amounts must fail")
	}
}

func TestFinanceApplyPostpaid(t *testing.T) {
	fi := Finance{Model: PaymentPostpaid, Currency: "USD", CreditLimit: 50_000_00}
	if _, err := fi.Apply(EntryTopUp, 1); err == nil {
		t.Fatal("top-ups only apply to prepaid")
	}
	if bal, _ := fi.Apply(EntryCharge, 49_000_00); bal != 49_000_00 {
		t.Fatalf("used = %d", bal)
	}
	if _, err := fi.Apply(EntryCharge, 1_000_01); err == nil {
		t.Fatal("charge over the limit must fail")
	}
	if fi.UsedPct() != 98 {
		t.Fatalf("used pct = %d", fi.UsedPct())
	}
	if _, err := fi.Apply(EntryPayment, 49_000_01); err == nil {
		t.Fatal("payment over the outstanding amount must fail")
	}
	if bal, _ := fi.Apply(EntryPayment, 9_000_00); bal != 40_000_00 {
		t.Fatalf("after payment = %d", bal)
	}
	unlimited := Finance{Model: PaymentPostpaid}
	if _, err := unlimited.Apply(EntryCharge, 9_999_999_00); err != nil || unlimited.Exhausted() {
		t.Fatal("a credit line without a limit is never exhausted")
	}
	card := Finance{Model: PaymentCard}
	if bal, err := card.Apply(EntryCharge, 100); err != nil || bal != 0 {
		t.Fatalf("card charge = %d %v", bal, err)
	}
	if _, err := card.Apply(EntryAdjustment, 1); err == nil {
		t.Fatal("card accounts have nothing to adjust")
	}
}

func TestAvailabilityBlocksAndWarns(t *testing.T) {
	base := func() *Supplier {
		s := validSupplier()
		_ = s.Normalize()
		s.Finance.DepositBalance = 10_000_00
		s.Health.Status = HealthActive
		return s
	}
	cases := []struct {
		name   string
		mutate func(*Supplier)
		reason string
		warn   string
	}{
		{"bookable", func(*Supplier) {}, "", ""},
		{"inactive", func(s *Supplier) { s.IsActive = false }, BlockInactive, ""},
		{"expired", func(s *Supplier) { s.ContractEnd = day(-1) }, BlockContractExpired, ""},
		{"down", func(s *Supplier) { s.Health.Status = HealthDown }, BlockDown, ""},
		{"deposit empty", func(s *Supplier) { s.Finance.DepositBalance = 0 }, BlockDepositEmpty, ""},
		{"credit full", func(s *Supplier) {
			s.Finance = Finance{Model: PaymentPostpaid, Currency: "USD", CreditLimit: 100, CreditUsed: 100}
		}, BlockCreditFull, ""},
		{"low", func(s *Supplier) { s.Finance.DepositBalance = 100_00 }, "", WarnLowBalance},
		{"expiring", func(s *Supplier) { s.ContractEnd = day(ContractWarnDays) }, "", WarnContractExpiring},
		{"degraded", func(s *Supplier) { s.Health.Status = HealthDegraded }, "", WarnDegraded},
	}
	for _, c := range cases {
		s := base()
		c.mutate(s)
		a := s.AvailabilityOn(today)
		if a.Reason != c.reason || a.Bookable != (c.reason == "") {
			t.Errorf("%s: got %+v", c.name, a)
		}
		if c.warn != "" && (len(a.Warnings) != 1 || a.Warnings[0] != c.warn) {
			t.Errorf("%s: warnings %v", c.name, a.Warnings)
		}
	}
}

func TestClassifyProbe(t *testing.T) {
	cases := []struct {
		latency time.Duration
		code    int
		err     error
		want    string
	}{
		{100 * time.Millisecond, 200, nil, HealthActive},
		{100 * time.Millisecond, 401, nil, HealthActive},
		{2 * time.Second, 200, nil, HealthDegraded},
		{100 * time.Millisecond, 503, nil, HealthDown},
		{0, 0, errors.New("timeout"), HealthDown},
	}
	for _, c := range cases {
		if got := ClassifyProbe(c.latency, c.code, c.err); got != c.want {
			t.Errorf("ClassifyProbe(%v,%d,%v) = %s, want %s", c.latency, c.code, c.err, got, c.want)
		}
	}
	var h Health
	if err := h.SetManual("maintenance", "", today); err == nil {
		t.Fatal("unknown manual status must fail")
	}
	if err := h.SetManual(" DOWN ", "planned", today); err != nil || h.Status != HealthDown || h.CheckedAt == nil {
		t.Fatalf("manual = %+v %v", h, err)
	}
}

func TestRankPrefersHealthyFundedSuppliers(t *testing.T) {
	mk := func(code, cat, health string, fi Finance) Supplier {
		return Supplier{Code: code, Category: cat, IsActive: true, Health: Health{Status: health}, Finance: fi,
			Markups: Markups{"flight": 300}}
	}
	funded := Finance{Model: PaymentPrepaid, DepositBalance: 100_000_00, LowBalanceThreshold: 1_000_00}
	thin := Finance{Model: PaymentPrepaid, DepositBalance: 1_500_00, LowBalanceThreshold: 1_000_00}
	empty := Finance{Model: PaymentPrepaid}
	opts := Rank([]Supplier{
		mk("C-EMPTY", CategoryGDS, HealthActive, empty),
		mk("B-THIN", CategoryGDS, HealthActive, thin),
		mk("A-SLOW", CategoryGDS, HealthDegraded, funded),
		mk("D-FUNDED", CategoryGDS, HealthActive, funded),
		mk("E-HOTEL", CategoryWholesaler, HealthActive, funded),
	}, ProductFlight, today)
	got := []string{}
	for _, o := range opts {
		got = append(got, o.Supplier.Code)
	}
	want := []string{"D-FUNDED", "B-THIN", "A-SLOW", "C-EMPTY"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
	if opts[len(opts)-1].Availability.Bookable || opts[0].MarkupBps != 300 {
		t.Fatalf("exhausted supplier must rank last and closed: %+v", opts[len(opts)-1])
	}
}

func TestUsageAndMetrics(t *testing.T) {
	u := Usage{Day: today, Searches: 1000, Bookings: 12}
	if err := u.Validate(today); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Usage{
		{Day: today.AddDate(0, 0, 1), Searches: 1},
		{Day: today.AddDate(0, 0, -MetricsWindowDays-1), Searches: 1},
		{Day: today},
		{Day: today, Searches: -1},
	} {
		if err := bad.Validate(today); err == nil {
			t.Errorf("usage %+v must be rejected", bad)
		}
	}
	m := Metrics{Searches: 2000, Bookings: 25, Errors: 40, PriceChanges: 2, SoldOuts: 3}
	if m.BookingsPer1000() != 12.5 || m.ErrorRatePct() != 2 {
		t.Fatalf("look-to-book %v error %v", m.BookingsPer1000(), m.ErrorRatePct())
	}
	if m.FailedBookingPct() != 16.7 {
		t.Fatalf("failed booking pct = %v", m.FailedBookingPct())
	}
	if (Metrics{}).BookingsPer1000() != 0 || (Metrics{}).FailedBookingPct() != 0 {
		t.Fatal("empty metrics must be zero")
	}
}

func TestDisputeLifecycle(t *testing.T) {
	d := &Dispute{Title: "  Overcharged room  ", Currency: "SAR", Amount: 250_00, Status: DisputeOpen}
	if err := d.Normalize(); err != nil || d.Title != "Overcharged room" {
		t.Fatalf("normalize = %v %q", err, d.Title)
	}
	if err := d.Close("open", "", today); err == nil {
		t.Fatal("closing to open must fail")
	}
	if err := d.Close(DisputeResolved, "Credit note issued", today); err != nil || d.ResolvedAt == nil {
		t.Fatalf("close = %v", err)
	}
	if err := d.Close(DisputeRejected, "", today); err == nil {
		t.Fatal("closed disputes cannot be closed again")
	}
	if err := (&Dispute{Title: "x", Currency: "SAR"}).Normalize(); err == nil {
		t.Fatal("short titles must fail")
	}
}

func TestContractDaysLeft(t *testing.T) {
	s := Supplier{ContractEnd: day(12)}
	if d, ok := s.ContractDaysLeft(today.Add(23 * time.Hour)); !ok || d != 12 {
		t.Fatalf("days = %d %v", d, ok)
	}
	if _, ok := (&Supplier{}).ContractDaysLeft(today); ok {
		t.Fatal("no end date means no countdown")
	}
}
