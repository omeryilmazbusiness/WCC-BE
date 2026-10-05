package finance

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func day(s string) time.Time {
	d, err := ParseDay(s)
	if err != nil {
		panic(err)
	}
	return d
}

func isKind(err error, kind string) bool {
	var ae *shared.AppError
	return errors.As(err, &ae) && string(ae.Code) == kind
}

func TestApplyBPSAndRatio(t *testing.T) {
	cases := []struct {
		amount int64
		bps    int
		want   int64
	}{{10_000, 175, 175}, {999, 175, 17}, {1, 5_000, 1}, {-1, 5_000, -1}, {0, 300, 0}}
	for _, c := range cases {
		if got := ApplyBPS(c.amount, c.bps); got != c.want {
			t.Errorf("ApplyBPS(%d,%d)=%d want %d", c.amount, c.bps, got, c.want)
		}
	}
	if got := RatioBPS(1, 3); got != 3333 {
		t.Errorf("RatioBPS(1,3)=%d", got)
	}
	if got := RatioBPS(-1, 3); got != -3333 {
		t.Errorf("RatioBPS(-1,3)=%d", got)
	}
	if RatioBPS(5, 0) != 0 {
		t.Error("ratio of zero whole")
	}
}

func TestAccountNormalize(t *testing.T) {
	a := Account{Kind: "BANK", Name: " Ziraat TRY ", Currency: "try", IBAN: "TR33 0006 1005 1978 6457 8413 26"}
	if err := a.Normalize(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if a.Currency != "TRY" || a.IBAN != "TR330006100519786457841326" || a.Name != "Ziraat TRY" {
		t.Fatalf("normalized: %+v", a)
	}
	bad := Account{Kind: "safe", Name: "", Currency: "xx", IBAN: "TR000000", CommissionBPS: 5}
	if err := bad.Normalize(); err == nil {
		t.Fatal("invalid account accepted")
	}
	pos := Account{Kind: "pos", Name: "Garanti POS", Currency: "TRY", CommissionBPS: 2_500}
	if err := pos.Normalize(); err == nil {
		t.Fatal("commission above cap accepted")
	}
	cash := Account{Kind: "cash", Name: "Till", Currency: "SAR", CommissionBPS: 100}
	if err := cash.Normalize(); err != nil || cash.CommissionBPS != 0 {
		t.Fatalf("cash commission should reset: %v %d", err, cash.CommissionBPS)
	}
}

func TestPostMovement(t *testing.T) {
	till := Account{ID: uuid.New(), Kind: AccountCash, Currency: "SAR", IsActive: true, Balance: 1_000}
	in := Movement{Direction: DirectionIn, Kind: MoveCollection, Amount: 500}
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := till.Post(&in); err != nil || till.Balance != 1_500 || in.BalanceAfter != 1_500 || in.Currency != "SAR" {
		t.Fatalf("post in: %v %+v", err, till)
	}
	out := Movement{Direction: DirectionOut, Kind: MoveExpense, Amount: 2_000}
	_ = out.Validate()
	if err := till.Post(&out); !isKind(err, "conflict") {
		t.Fatalf("overdraft on till: %v", err)
	}
	bank := Account{Kind: AccountBank, Currency: "SAR", IsActive: true}
	if err := bank.Post(&out); err != nil || bank.Balance != -2_000 {
		t.Fatalf("bank may mirror an overdraft: %v %d", err, bank.Balance)
	}
	wrong := Movement{Direction: DirectionIn, Kind: MoveCollection, Amount: 1, Currency: "USD"}
	if err := till.Post(&wrong); err == nil {
		t.Fatal("currency mismatch accepted")
	}
	closed := Account{Kind: AccountBank, Currency: "SAR"}
	if err := closed.Post(&in); err == nil {
		t.Fatal("inactive account accepted a movement")
	}
}

func TestMovementValidate(t *testing.T) {
	m := Movement{Direction: DirectionOut, Kind: MoveCollection, Amount: 10}
	if m.Validate() == nil {
		t.Fatal("outgoing collection accepted")
	}
	m = Movement{Direction: DirectionIn, Kind: MoveCollection, Amount: 10, Fee: 11}
	if m.Validate() == nil {
		t.Fatal("fee above amount accepted")
	}
	feed := Movement{Direction: DirectionIn, Kind: MoveCollection, Amount: 10, Source: SourceBankFeed}
	if err := feed.Validate(); err != nil || feed.MatchStatus != MatchUnmatched {
		t.Fatalf("bank feed in should await matching: %v %s", err, feed.MatchStatus)
	}
	manual := Movement{Direction: DirectionIn, Kind: MoveCollection, Amount: 10}
	_ = manual.Validate()
	if manual.MatchStatus != MatchNA {
		t.Fatalf("manual match status %s", manual.MatchStatus)
	}
}

func TestPOSFeeAndNet(t *testing.T) {
	pos := Account{Kind: AccountPOS, CommissionBPS: 179}
	fee := pos.POSFee(100_000)
	if fee != 1_790 {
		t.Fatalf("fee %d", fee)
	}
	m := Movement{Direction: DirectionIn, Amount: 100_000, Fee: fee}
	if m.Net() != 98_210 {
		t.Fatalf("net %d", m.Net())
	}
	out := Movement{Direction: DirectionOut, Amount: 1_000, Fee: 5}
	if out.Net() != -1_005 {
		t.Fatalf("out net %d", out.Net())
	}
	if (Account{Kind: AccountBank, CommissionBPS: 100}).POSFee(1_000) != 0 {
		t.Fatal("bank charged POS fee")
	}
}

func TestAgencyRiskAndSuspension(t *testing.T) {
	a := Agency{Code: "ag-01", Name: "Delta Travel", Currency: "sar", CreditLimit: 100_000, GraceDays: 5, AutoSuspend: true, Status: AgencyActive}
	if err := a.Normalize(); err != nil {
		t.Fatal(err)
	}
	if a.Code != "AG-01" || a.Currency != "SAR" {
		t.Fatalf("normalize %+v", a)
	}
	cases := []struct {
		e    Exposure
		want string
	}{
		{Exposure{Outstanding: 10_000}, RiskOK},
		{Exposure{Outstanding: 75_000}, RiskWatch},
		{Exposure{Outstanding: 10_000, Overdue: 1, OldestOverdueDays: 2}, RiskWatch},
		{Exposure{Outstanding: 95_000}, RiskCritical},
		{Exposure{Outstanding: 10_000, Overdue: 1, OldestOverdueDays: 6}, RiskCritical},
	}
	for _, c := range cases {
		if got := a.RiskOf(c.e).Level; got != c.want {
			t.Errorf("risk %+v = %s want %s", c.e, got, c.want)
		}
	}
	if !a.ShouldSuspend(Exposure{Overdue: 1, OldestOverdueDays: 6}) {
		t.Fatal("past grace should suspend")
	}
	if a.ShouldSuspend(Exposure{Overdue: 1, OldestOverdueDays: 5}) {
		t.Fatal("within grace suspended")
	}
	if err := a.CanSell(Exposure{Outstanding: 90_000}, 20_000); !isKind(err, "conflict") {
		t.Fatalf("limit breach: %v", err)
	}
	if err := a.CanSell(Exposure{Outstanding: 90_000}, 10_000); err != nil {
		t.Fatalf("exactly at limit: %v", err)
	}
	now := time.Now()
	if err := a.Suspend(SuspendOverdue, now); err != nil || a.RiskOf(Exposure{}).Level != RiskBlocked {
		t.Fatalf("suspend: %v", err)
	}
	if err := a.CanSell(Exposure{}, 1); err == nil {
		t.Fatal("suspended agency sold")
	}
	if err := a.Reactivate(now); err != nil || a.Status != AgencyActive || a.SuspendReason != "" {
		t.Fatalf("reactivate: %v %+v", err, a)
	}
	prepaid := Agency{Status: AgencyActive}
	if err := prepaid.CanSell(Exposure{Outstanding: 1_000_000}, 1); err != nil {
		t.Fatalf("no-limit agency blocked: %v", err)
	}
}

func TestAgeing(t *testing.T) {
	today := day("2026-10-05")
	cases := map[string]string{
		"2026-10-10": AgeCurrent, "2026-10-05": AgeCurrent, "2026-10-04": Age0to15,
		"2026-09-20": Age0to15, "2026-09-19": Age16to30, "2026-09-05": Age16to30, "2026-09-04": Age31Plus,
	}
	for due, want := range cases {
		if got := BucketOf(day(due), today); got != want {
			t.Errorf("due %s: %s want %s", due, got, want)
		}
	}
	a := NewAgeing("SAR")
	a.Add(AgeCurrent, 100)
	a.Add(Age0to15, 50)
	a.Add(Age31Plus, 25)
	a.Add(AgeUnscheduled, 10)
	if a.Total() != 185 || a.Overdue() != 75 || a.Counts[Age31Plus] != 1 {
		t.Fatalf("ageing %+v", a)
	}
}

func TestPnLAndCommission(t *testing.T) {
	p := PnL{Gross: 12_000, Net: 9_000, Tax: 1_000, Fee: 0, Costed: true}
	if p.Revenue() != 11_000 || p.Margin() != 2_000 || p.MarginBPS() != 1818 {
		t.Fatalf("pnl %d %d %d", p.Revenue(), p.Margin(), p.MarginBPS())
	}
	if (PnL{Gross: 5_000}).Margin() != 0 {
		t.Fatal("uncosted sale showed margin")
	}
	if Commission(2_000, 1_000) != 200 || Commission(-500, 1_000) != 0 || Commission(2_000, 0) != 0 {
		t.Fatal("commission")
	}
	r := RepEarnings{}
	r.Settle([]int64{2_000, -1_000, 500}, 1_000)
	if r.Commission != 250 {
		t.Fatalf("losses must not claw back: %d", r.Commission)
	}
	b := Budget{Currency: "SAR", Revenue: 100_000, Cost: 80_000}
	if err := b.Normalize(); err != nil {
		t.Fatal(err)
	}
	v := VarianceOf(b, PnL{Gross: 110_000, Net: 85_000, Costed: true})
	if v.Revenue != 10_000 || v.Cost != 5_000 || v.Margin != 5_000 || !v.CostOverrun {
		t.Fatalf("variance %+v", v)
	}
}

func TestShares(t *testing.T) {
	out := Shares([]CurrencyShare{
		{Currency: "USD", Converted: 300},
		{Currency: "SAR", Converted: 400},
		{Currency: "TRY", Converted: 200},
		{Currency: "EUR", Converted: 100},
		{Currency: "GBP", Converted: 999, Unconverted: true},
		{Currency: "AED", Converted: -50},
	})
	want := map[string]int{"SAR": 4000, "USD": 3000, "TRY": 2000, "EUR": 1000, "GBP": 0, "AED": 0}
	sum := 0
	for _, s := range out {
		if s.ShareBPS != want[s.Currency] {
			t.Errorf("%s=%d want %d", s.Currency, s.ShareBPS, want[s.Currency])
		}
		sum += s.ShareBPS
	}
	if sum != BPS {
		t.Fatalf("shares sum %d", sum)
	}
	thirds := Shares([]CurrencyShare{{Currency: "A", Converted: 1}, {Currency: "B", Converted: 1}, {Currency: "C", Converted: 1}})
	total := 0
	for _, s := range thirds {
		total += s.ShareBPS
	}
	if total != BPS {
		t.Fatalf("largest remainder sum %d", total)
	}
}

func TestSettleRefund(t *testing.T) {
	s := SettleRefund(RefundInput{Paid: 10_000, SupplierCost: 8_000, SupplierPenalty: 2_000, ServiceFee: 500})
	if s.CustomerRefund != 7_500 || s.SupplierRefund != 6_000 || s.Retained != 2_500 || s.Shortfall != 0 {
		t.Fatalf("settlement %+v", s)
	}
	// 10 000 in + 6 000 back - 8 000 paid - 7 500 refunded = 500 (the fee).
	if s.AgencyResult != 500 {
		t.Fatalf("agency result %d", s.AgencyResult)
	}
	short := SettleRefund(RefundInput{Paid: 1_000, SupplierCost: 8_000, SupplierPenalty: 2_000})
	if short.CustomerRefund != 0 || short.Shortfall != 1_000 {
		t.Fatalf("shortfall %+v", short)
	}
	if (RefundInput{SupplierCost: 100, SupplierPenalty: 200}).Validate() == nil {
		t.Fatal("penalty above cost accepted")
	}
}

func TestReconcileBSP(t *testing.T) {
	st := &BSPStatement{ID: uuid.New(), Currency: "USD"}
	b1, b2, b3 := uuid.New(), uuid.New(), uuid.New()
	lines := []BSPLine{
		{DocumentNo: "0651234567890", PNR: "abc123", Amount: 500},
		{DocumentNo: "0651234567891", PNR: "ABC123", Amount: 500},
		{DocumentNo: "0651234567892", PNR: "DEF456", Amount: 700},
		{DocumentNo: "0651234567893", PNR: "ZZZ999", Amount: 300},
		{DocumentNo: "0651234567894", PNR: "DEF456", Type: "refund", Amount: 100},
	}
	if err := NormalizeLines(lines); err != nil {
		t.Fatal(err)
	}
	tickets := []SystemTicket{
		{BookingID: b1, PNR: "ABC123", Amount: 1_000, Currency: "USD"},
		{BookingID: b2, PNR: "DEF456", Amount: 650, Currency: "USD"},
		{BookingID: b3, PNR: "GHI789", Amount: 400, Currency: "USD"},
		{BookingID: uuid.New(), PNR: "EUR111", Amount: 1, Currency: "EUR"},
	}
	out := Reconcile(st, lines, tickets)
	status := map[string]int{}
	for _, l := range out {
		status[l.Status]++
	}
	if status[BSPMatched] != 2 || status[BSPAmountMismatch] != 2 || status[BSPMissingInSystem] != 1 || status[BSPMissingInBSP] != 1 {
		t.Fatalf("statuses %v", status)
	}
	if st.Total != 1_900 || st.SystemTotal != 2_050 || st.Discrepancy() != -150 || st.LineCount != 6 {
		t.Fatalf("totals %+v", st)
	}
	if NormalizeLines(nil) == nil {
		t.Fatal("empty statement accepted")
	}
}

func TestLetter(t *testing.T) {
	tok, hash, err := NewLetterToken()
	if err != nil || len(tok) < 40 || HashLetterToken(tok) != hash || HashLetterToken(tok+"x") == hash {
		t.Fatalf("token %v", err)
	}
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	l := Letter{Status: LetterSent, ExpiresAt: now.Add(LetterTTL)}
	if l.Respond(false, "Ali", "", now) == nil {
		t.Fatal("dispute without note accepted")
	}
	if err := l.Respond(true, "Ali Veli", "", now); err != nil || l.Status != LetterConfirmed || l.RespondedAt == nil {
		t.Fatalf("confirm: %v", err)
	}
	if l.Respond(true, "Ali", "", now) == nil {
		t.Fatal("answered twice")
	}
	old := Letter{Status: LetterSent, ExpiresAt: now.Add(-time.Hour)}
	if old.StatusOn(now) != LetterExpired || old.Respond(true, "Ali", "", now) == nil {
		t.Fatal("expired letter answered")
	}
}
