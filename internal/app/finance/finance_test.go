package finance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var fixedNow = time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

func base() Base { return Base{Location: time.UTC, Now: func() time.Time { return fixedNow }} }

// ---- fakes ----

type memTreasury struct {
	accounts  map[uuid.UUID]*domain.Account
	movements []*domain.Movement
}

func newMemTreasury() *memTreasury { return &memTreasury{accounts: map[uuid.UUID]*domain.Account{}} }

func (m *memTreasury) ListAccounts(context.Context, *uuid.UUID) ([]domain.Account, error) {
	var out []domain.Account
	for _, a := range m.accounts {
		out = append(out, *a)
	}
	return out, nil
}
func (m *memTreasury) FindAccount(_ context.Context, id uuid.UUID) (*domain.Account, error) {
	a, ok := m.accounts[id]
	if !ok {
		return nil, shared.NewNotFound("account")
	}
	c := *a
	return &c, nil
}
func (m *memTreasury) LockAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error) {
	return m.FindAccount(ctx, id)
}
func (m *memTreasury) InsertAccount(_ context.Context, a *domain.Account) error {
	c := *a
	m.accounts[a.ID] = &c
	return nil
}
func (m *memTreasury) UpdateAccount(_ context.Context, a *domain.Account) error {
	c := *a
	m.accounts[a.ID] = &c
	return nil
}
func (m *memTreasury) SetBalance(_ context.Context, id uuid.UUID, b int64) error {
	m.accounts[id].Balance = b
	return nil
}
func (m *memTreasury) InsertMovement(_ context.Context, mv *domain.Movement) (bool, error) {
	if mv.ExternalID != "" {
		for _, x := range m.movements {
			if x.AccountID == mv.AccountID && x.ExternalID == mv.ExternalID {
				return false, nil
			}
		}
	}
	c := *mv
	m.movements = append(m.movements, &c)
	return true, nil
}
func (m *memTreasury) FindMovement(_ context.Context, id uuid.UUID) (*domain.Movement, error) {
	for _, x := range m.movements {
		if x.ID == id {
			c := *x
			return &c, nil
		}
	}
	return nil, shared.NewNotFound("movement")
}
func (m *memTreasury) ListMovements(context.Context, MovementFilter) ([]domain.Movement, error) {
	return nil, nil
}
func (m *memTreasury) ResolveMatch(_ context.Context, id uuid.UUID, status string, b, p *uuid.UUID) error {
	for _, x := range m.movements {
		if x.ID == id {
			if x.MatchStatus != domain.MatchUnmatched {
				return errors.New("already resolved")
			}
			x.MatchStatus, x.BookingID, x.MatchedPaymentID = status, b, p
		}
	}
	return nil
}
func (m *memTreasury) POSStats(context.Context, *uuid.UUID, time.Time) ([]POSStat, error) {
	return nil, nil
}

type memBookings struct{ due map[uuid.UUID]*BookingFacts }

func (m *memBookings) BookingsByRefs(_ context.Context, branch uuid.UUID, refs []int64, pnrs []string) ([]domain.BookingDue, error) {
	var out []domain.BookingDue
	for _, b := range m.due {
		if b.BranchID != branch {
			continue
		}
		hit := false
		for _, r := range refs {
			hit = hit || r == b.RefNo
		}
		for _, p := range pnrs {
			hit = hit || p == b.PNR
		}
		if hit {
			out = append(out, domain.BookingDue{BookingID: b.ID, RefNo: b.RefNo, PNR: b.PNR, Currency: b.Currency, Balance: b.Balance})
		}
	}
	return out, nil
}
func (m *memBookings) BookingDue(_ context.Context, id uuid.UUID) (*BookingFacts, error) {
	b, ok := m.due[id]
	if !ok {
		return nil, shared.NewNotFound("booking")
	}
	c := *b
	return &c, nil
}

type memPayments struct{ recorded []apppayment.RecordInput }

func (m *memPayments) Record(_ context.Context, in apppayment.RecordInput) (*paymentdomain.Payment, error) {
	m.recorded = append(m.recorded, in)
	return &paymentdomain.Payment{ID: uuid.New(), BookingID: in.BookingID, Amount: in.Amount}, nil
}

// ---- treasury ----

func TestTreasuryOpeningBalanceTransferAndPOSFee(t *testing.T) {
	st := newMemTreasury()
	svc := NewTreasuryService(base(), st, &memBookings{}, &memPayments{}, tx.Nop{})
	ctx := context.Background()
	branch, actor := uuid.New(), uuid.New()

	till, err := svc.CreateAccount(ctx, branch, actor, AccountInput{Kind: "cash", Name: "Main till", Currency: "SAR", OpeningBalance: 50_000})
	if err != nil || st.accounts[till.ID].Balance != 50_000 || len(st.movements) != 1 {
		t.Fatalf("opening balance: %v", err)
	}
	bank, _ := svc.CreateAccount(ctx, branch, actor, AccountInput{Kind: "bank", Name: "Al Rajhi", Currency: "SAR"})
	if _, err := svc.Transfer(ctx, actor, TransferInput{FromID: till.ID, ToID: bank.ID, Amount: 20_000}); err != nil {
		t.Fatal(err)
	}
	if st.accounts[till.ID].Balance != 30_000 || st.accounts[bank.ID].Balance != 20_000 {
		t.Fatalf("transfer balances %d %d", st.accounts[till.ID].Balance, st.accounts[bank.ID].Balance)
	}
	if _, err := svc.Transfer(ctx, actor, TransferInput{FromID: till.ID, ToID: bank.ID, Amount: 40_000}); err == nil {
		t.Fatal("till overdraft through transfer accepted")
	}
	usd, _ := svc.CreateAccount(ctx, branch, actor, AccountInput{Kind: "bank", Name: "USD", Currency: "USD"})
	if _, err := svc.Transfer(ctx, actor, TransferInput{FromID: bank.ID, ToID: usd.ID, Amount: 1}); err == nil {
		t.Fatal("cross-currency transfer accepted")
	}

	pos, _ := svc.CreateAccount(ctx, branch, actor, AccountInput{Kind: "pos", Name: "POS", Currency: "SAR", CommissionBPS: 200})
	m, acc, err := svc.Post(ctx, actor, MovementInput{AccountID: pos.ID, Direction: "in", Kind: "collection", Amount: 10_000})
	if err != nil || m.Fee != 200 || acc.Balance != 9_800 {
		t.Fatalf("pos fee: %v %+v", err, m)
	}
	if _, _, err := svc.Post(ctx, actor, MovementInput{AccountID: pos.ID, Direction: "in", Kind: "collection", Amount: 1, OccurredOn: "2030-01-01"}); err == nil {
		t.Fatal("future-dated movement accepted")
	}
}

func TestImportFeedDedupesAndAutoMatches(t *testing.T) {
	st := newMemTreasury()
	branch, actor := uuid.New(), uuid.New()
	b1 := &BookingFacts{ID: uuid.New(), BranchID: branch, RefNo: 1042, Currency: "SAR", Balance: 5_000}
	b2 := &BookingFacts{ID: uuid.New(), BranchID: branch, PNR: "X7K2LP", Currency: "SAR", Balance: 1_000}
	pays := &memPayments{}
	svc := NewTreasuryService(base(), st, &memBookings{due: map[uuid.UUID]*BookingFacts{b1.ID: b1, b2.ID: b2}}, pays, tx.Nop{})
	ctx := context.Background()
	bank, _ := svc.CreateAccount(ctx, branch, actor, AccountInput{Kind: "bank", Name: "Bank", Currency: "SAR"})
	rows := []FeedRow{
		{ExternalID: "T1", Amount: 3_000, Direction: "in", Description: "EFT WCC-1042 umre"},
		{ExternalID: "T2", Amount: 2_000, Direction: "in", Description: "PNR X7K2LP"},
		{ExternalID: "T3", Amount: 700, Direction: "out", Description: "Bank fee"},
		{ExternalID: "T4", Amount: 900, Direction: "in", Description: "unknown sender"},
	}
	res, err := svc.ImportFeed(ctx, bank.ID, actor, true, rows)
	if err != nil {
		t.Fatal(err)
	}
	if res.Imported != 4 || res.AutoMatched != 1 || res.Unmatched != 2 || res.Duplicates != 0 {
		t.Fatalf("first import %+v", res)
	}
	if len(pays.recorded) != 1 || pays.recorded[0].BookingID != b1.ID || pays.recorded[0].Method != "bank_transfer" || !pays.recorded[0].AutoVerify {
		t.Fatalf("payment %+v", pays.recorded)
	}
	if st.accounts[bank.ID].Balance != 3_000+2_000-700+900 {
		t.Fatalf("balance %d", st.accounts[bank.ID].Balance)
	}
	again, err := svc.ImportFeed(ctx, bank.ID, actor, true, rows)
	if err != nil || again.Duplicates != 4 || again.Imported != 0 {
		t.Fatalf("re-import %+v %v", again, err)
	}
	if st.accounts[bank.ID].Balance != 5_200 {
		t.Fatalf("re-import changed balance: %d", st.accounts[bank.ID].Balance)
	}
	var unknown *domain.Movement
	for _, m := range st.movements {
		if m.ExternalID == "T4" {
			unknown = m
		}
	}
	if _, err := svc.Match(ctx, unknown.ID, b2.ID, actor, true); err != nil {
		t.Fatalf("manual match: %v", err)
	}
	if _, err := svc.Match(ctx, unknown.ID, b2.ID, actor, true); err == nil {
		t.Fatal("matched twice")
	}
	if _, err := svc.ImportFeed(ctx, bank.ID, actor, true, []FeedRow{{Amount: 1, Direction: "in"}}); err == nil {
		t.Fatal("row without bank id accepted")
	}
}

// ---- receivables ----

type memAgencies struct {
	list     map[uuid.UUID]*domain.Agency
	exposure map[uuid.UUID]domain.Exposure
	assigned map[uuid.UUID]*uuid.UUID
}

func (m *memAgencies) ListAgencies(context.Context, *uuid.UUID) ([]domain.Agency, error) {
	var out []domain.Agency
	for _, a := range m.list {
		out = append(out, *a)
	}
	return out, nil
}
func (m *memAgencies) FindAgency(_ context.Context, id uuid.UUID) (*domain.Agency, error) {
	a, ok := m.list[id]
	if !ok {
		return nil, shared.NewNotFound("agency")
	}
	c := *a
	return &c, nil
}
func (m *memAgencies) LockAgency(ctx context.Context, id uuid.UUID) (*domain.Agency, error) {
	return m.FindAgency(ctx, id)
}
func (m *memAgencies) InsertAgency(_ context.Context, a *domain.Agency) error {
	c := *a
	m.list[a.ID] = &c
	return nil
}
func (m *memAgencies) UpdateAgency(_ context.Context, a *domain.Agency) error {
	c := *a
	m.list[a.ID] = &c
	return nil
}
func (m *memAgencies) Exposures(context.Context, *uuid.UUID, time.Time) (map[uuid.UUID]domain.Exposure, error) {
	return m.exposure, nil
}
func (m *memAgencies) SetBookingAgency(_ context.Context, b uuid.UUID, a *uuid.UUID) error {
	m.assigned[b] = a
	return nil
}
func (m *memAgencies) ListActiveForSweep(context.Context) ([]domain.Agency, error) {
	var out []domain.Agency
	for _, a := range m.list {
		if a.Status == domain.AgencyActive {
			out = append(out, *a)
		}
	}
	return out, nil
}

func TestAgencyAssignAndSweep(t *testing.T) {
	branch, actor := uuid.New(), uuid.New()
	ag := &memAgencies{list: map[uuid.UUID]*domain.Agency{}, exposure: map[uuid.UUID]domain.Exposure{}, assigned: map[uuid.UUID]*uuid.UUID{}}
	big := &BookingFacts{ID: uuid.New(), BranchID: branch, Currency: "SAR", Balance: 80_000}
	small := &BookingFacts{ID: uuid.New(), BranchID: branch, Currency: "SAR", Balance: 10_000}
	usd := &BookingFacts{ID: uuid.New(), BranchID: branch, Currency: "USD", Balance: 1}
	bk := &memBookings{due: map[uuid.UUID]*BookingFacts{big.ID: big, small.ID: small, usd.ID: usd}}
	svc := NewReceivablesService(base(), ag, nil, bk, tx.Nop{})
	ctx := context.Background()

	a, err := svc.CreateAgency(ctx, branch, actor, AgencyInput{Code: "dlt", Name: "Delta", Currency: "SAR", CreditLimit: 100_000, GraceDays: 3})
	if err != nil || !a.AutoSuspend {
		t.Fatalf("create: %v", err)
	}
	ag.exposure[a.ID] = domain.Exposure{Outstanding: 30_000}
	if err := svc.AssignBooking(ctx, a.ID, big.ID, actor); err == nil {
		t.Fatal("assignment past the credit limit accepted")
	}
	if err := svc.AssignBooking(ctx, a.ID, small.ID, actor); err != nil || ag.assigned[small.ID] == nil {
		t.Fatalf("assign: %v", err)
	}
	small.AgencyID = ag.assigned[small.ID]
	if err := svc.AssignBooking(ctx, a.ID, usd.ID, actor); err == nil {
		t.Fatal("currency mismatch accepted")
	}

	ag.exposure[a.ID] = domain.Exposure{Outstanding: 30_000, Overdue: 5_000, OldestOverdueDays: 10}
	n, err := svc.SweepOverdue(ctx)
	if err != nil || n != 1 || ag.list[a.ID].Status != domain.AgencySuspended || ag.list[a.ID].SuspendReason != domain.SuspendOverdue {
		t.Fatalf("sweep: %d %v %+v", n, err, ag.list[a.ID])
	}
	if err := svc.AssignBooking(ctx, a.ID, small.ID, actor); err != nil {
		t.Fatalf("re-assigning the same booking is a no-op: %v", err)
	}
	other := &BookingFacts{ID: uuid.New(), BranchID: branch, Currency: "SAR", Balance: 1}
	bk.due[other.ID] = other
	if err := svc.AssignBooking(ctx, a.ID, other.ID, actor); err == nil {
		t.Fatal("suspended agency took a booking")
	}
	if _, err := svc.SetStatus(ctx, a.ID, actor, domain.AgencyActive); err != nil || ag.list[a.ID].Status != domain.AgencyActive {
		t.Fatalf("reactivate: %v", err)
	}
	views, _ := svc.Agencies(ctx, nil)
	if len(views) != 1 || views[0].Risk.Level != domain.RiskCritical {
		t.Fatalf("views %+v", views)
	}
}

// ---- reconciliation ----

type memLetters struct{ list map[string]*domain.Letter }

func (m *memLetters) InsertLetter(_ context.Context, l *domain.Letter) error {
	c := *l
	m.list[l.TokenHash] = &c
	return nil
}
func (m *memLetters) ListLetters(context.Context, *uuid.UUID, int) ([]domain.Letter, error) {
	return nil, nil
}
func (m *memLetters) FindLetterByToken(ctx context.Context, h string) (*domain.Letter, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, err
	}
	l, ok := m.list[h]
	if !ok {
		return nil, shared.NewNotFound("letter")
	}
	c := *l
	return &c, nil
}
func (m *memLetters) SaveLetterResponse(_ context.Context, l *domain.Letter) error {
	c := *l
	m.list[l.TokenHash] = &c
	return nil
}
func (m *memLetters) BranchName(context.Context, uuid.UUID) (string, error) {
	return "WODI Travel", nil
}

func TestLetterRoundTripAndRefundQuote(t *testing.T) {
	branch, actor := uuid.New(), uuid.New()
	ag := &memAgencies{list: map[uuid.UUID]*domain.Agency{}, exposure: map[uuid.UUID]domain.Exposure{}}
	agency := &domain.Agency{ID: uuid.New(), BranchID: branch, Name: "Delta", Currency: "SAR", Email: "acc@delta.test"}
	ag.list[agency.ID] = agency
	ag.exposure[agency.ID] = domain.Exposure{Outstanding: 42_000}
	letters := &memLetters{list: map[string]*domain.Letter{}}
	bk := &BookingFacts{ID: uuid.New(), BranchID: branch, Currency: "SAR", Collected: 10_000, Cost: 8_000}
	svc := NewReconService(base(), nil, letters, &memBookings{due: map[uuid.UUID]*BookingFacts{bk.ID: bk}}, ag, nil, tx.Nop{})
	ctx := context.Background()

	other := uuid.New()
	if _, _, err := svc.CreateLetter(ctx, &other, actor, LetterInput{PartyType: "agency", PartyID: agency.ID}); err == nil {
		t.Fatal("letter for another branch accepted")
	}
	l, token, err := svc.CreateLetter(ctx, &branch, actor, LetterInput{PartyType: "agency", PartyID: agency.ID})
	if err != nil || l.Balance != 42_000 || l.Email != "acc@delta.test" || token == "" || l.TokenHash == token {
		t.Fatalf("create letter: %v %+v", err, l)
	}
	pl, err := svc.LetterByToken(ctx, token)
	if err != nil || pl.Company != "WODI Travel" || pl.Letter.ID != l.ID {
		t.Fatalf("public read: %v", err)
	}
	if _, err := svc.LetterByToken(ctx, "short"); err == nil {
		t.Fatal("short token resolved")
	}
	if _, err := svc.RespondLetter(ctx, token, LetterResponse{Confirm: false, Name: "Ali"}); err == nil {
		t.Fatal("dispute without note accepted")
	}
	got, err := svc.RespondLetter(ctx, token, LetterResponse{Confirm: true, Name: "Ali Veli"})
	if err != nil || got.Letter.Status != domain.LetterConfirmed {
		t.Fatalf("respond: %v", err)
	}

	q, err := svc.QuoteRefund(ctx, RefundQuoteInput{BookingID: &bk.ID, SupplierPenalty: 2_000, ServiceFee: 500})
	if err != nil || q.Currency != "SAR" || q.Settlement.CustomerRefund != 7_500 || q.Settlement.SupplierRefund != 6_000 {
		t.Fatalf("quote: %v %+v", err, q)
	}
	if _, err := svc.QuoteRefund(ctx, RefundQuoteInput{SupplierPenalty: 1}); err == nil {
		t.Fatal("quote without currency accepted")
	}
}
