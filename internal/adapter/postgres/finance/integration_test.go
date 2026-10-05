package finance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	pgbooking "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/booking"
	pgpayment "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/payment"
	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var errRollback = errors.New("rollback")

type sameRate struct{}

func (sameRate) Convert(_ context.Context, amount int64, from, to string, _ time.Time) (fxdomain.Conversion, error) {
	if from != to {
		return fxdomain.Conversion{}, errors.New("no rate")
	}
	return fxdomain.Conversion{Amount: amount}, nil
}

type seed struct {
	branch, owner, customer, departure uuid.UUID
	pkgBooking, flightBooking          uuid.UUID
	pkgRef                             int64
}

// TestFinanceHubAgainstPostgres drives every finance service over the real
// adapter inside one transaction that is rolled back. It needs a migrated
// database with at least one booking to borrow branch/customer/departure from.
func TestFinanceHubAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("FINANCE_IT_DATABASE_URL")
	if dsn == "" {
		t.Skip("FINANCE_IT_DATABASE_URL not set")
	}
	ctx := access.WithScope(context.Background(), access.System())
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	txm := tx.NewManager(pool)

	err = txm.WithinTransaction(ctx, func(ctx context.Context) error {
		s := seedBookings(ctx, t, pool)
		exercise(ctx, t, pool, txm, s)
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
}

func seedBookings(ctx context.Context, t *testing.T, pool *pgxpool.Pool) seed {
	t.Helper()
	q := tx.QuerierFrom(ctx, pool)
	var s seed
	if err := q.QueryRow(ctx, `SELECT branch_id, owner_id, customer_id, departure_id FROM bookings
		WHERE departure_id IS NOT NULL LIMIT 1`).Scan(&s.branch, &s.owner, &s.customer, &s.departure); err != nil {
		t.Skipf("no booking to borrow from: %v", err)
	}
	insert := func(service, pnr string, total, cost int64) (uuid.UUID, int64) {
		var id uuid.UUID
		var ref int64
		if err := q.QueryRow(ctx, `INSERT INTO bookings (branch_id, customer_id, departure_id, status, pax_count,
			total_amount, collected_amt, balance_amt, currency, owner_id, service_type, pnr, cost_amt)
			VALUES ($1,$2,$3,'confirmed',1,$4,0,$4,'SAR',$5,$6,$7,$8) RETURNING id, ref_no`,
			s.branch, s.customer, s.departure, total, s.owner, service, pnr, cost).Scan(&id, &ref); err != nil {
			t.Fatalf("seed booking: %v", err)
		}
		return id, ref
	}
	s.pkgBooking, s.pkgRef = insert("package", "", 500_000, 400_000)
	s.flightBooking, _ = insert("flight", "QA7X9Z", 120_000, 100_000)
	return s
}

func must[T any](t *testing.T, step string) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", step, err)
		}
		return v
	}
}

func exercise(ctx context.Context, t *testing.T, pool *pgxpool.Pool, txm *tx.Manager, s seed) {
	repo := NewRepository(pool)
	payments := apppayment.NewService(pgpayment.NewRepository(pool), pgbooking.NewRepository(pool, nil), txm, events.NewBus(slog.Default()))
	base := app.Base{Log: slog.Default(), Location: time.UTC}
	treasury := app.NewTreasuryService(base, repo, repo, payments, txm)
	receivables := app.NewReceivablesService(base, repo, repo, repo, txm)
	overview := app.NewOverviewService(base, repo, repo, sameRate{})
	profit := app.NewProfitService(base, repo, repo, repo, txm)
	recon := app.NewReconService(base, repo, repo, repo, repo, nil, txm)
	actor := s.owner
	today := time.Now().UTC().Format(time.DateOnly)

	// Treasury: accounts, opening balance, transfer, POS fee.
	bank := must[*domain.Account](t, "bank")(treasury.CreateAccount(ctx, s.branch, actor, app.AccountInput{
		Kind: "bank", Name: "IT Bank", Currency: "SAR", IBAN: "SA0380000000608010167519", OpeningBalance: 1_000_000,
	}))
	till := must[*domain.Account](t, "till")(treasury.CreateAccount(ctx, s.branch, actor, app.AccountInput{
		Kind: "cash", Name: "IT Till", Currency: "SAR",
	}))
	pos := must[*domain.Account](t, "pos")(treasury.CreateAccount(ctx, s.branch, actor, app.AccountInput{
		Kind: "pos", Name: "IT POS", Currency: "SAR", CommissionBPS: 175,
	}))
	must[[]domain.Movement](t, "transfer")(treasury.Transfer(ctx, actor, app.TransferInput{FromID: bank.ID, ToID: till.ID, Amount: 50_000, OccurredOn: today}))
	if a := must[*domain.Account](t, "find till")(repo.FindAccount(ctx, till.ID)); a.Balance != 50_000 {
		t.Fatalf("till balance %d", a.Balance)
	}
	must[*domain.Movement](t, "pos post")(func() (*domain.Movement, error) {
		m, _, err := treasury.Post(ctx, actor, app.MovementInput{AccountID: pos.ID, Direction: "in", Kind: "collection", Amount: 100_000, OccurredOn: today})
		return m, err
	}())
	stats := must[[]app.POSStat](t, "pos stats")(treasury.POSStats(ctx, &s.branch))
	found := false
	for _, st := range stats {
		if st.AccountID == pos.ID {
			found = st.Fees == 1_750 && st.Gross == 100_000
		}
	}
	if !found {
		t.Fatalf("pos stats %+v", stats)
	}

	// Bank feed: dedupe and auto-match by booking reference.
	rows := []app.FeedRow{
		{ExternalID: "it-1", OccurredOn: today, Amount: 200_000, Direction: "in", Description: "EFT WCC-" + strconv.FormatInt(s.pkgRef, 10)},
		{ExternalID: "it-2", OccurredOn: today, Amount: 9_999, Direction: "in", Description: "unknown sender"},
	}
	res := must[*app.FeedResult](t, "feed")(treasury.ImportFeed(ctx, bank.ID, actor, true, rows))
	if res.Imported != 2 || res.AutoMatched != 1 || res.Unmatched != 1 {
		t.Fatalf("feed result %+v", res)
	}
	res = must[*app.FeedResult](t, "feed again")(treasury.ImportFeed(ctx, bank.ID, actor, true, rows))
	if res.Duplicates != 2 || res.Imported != 0 {
		t.Fatalf("feed dedupe %+v", res)
	}
	unmatched := must[[]domain.Movement](t, "unmatched")(treasury.Movements(ctx, app.MovementFilter{AccountID: &bank.ID, UnmatchedOnly: true}))
	if len(unmatched) != 1 {
		t.Fatalf("unmatched = %d", len(unmatched))
	}
	if err := treasury.Ignore(ctx, unmatched[0].ID, actor); err != nil {
		t.Fatal(err)
	}
	facts := must[*app.BookingFacts](t, "booking due")(repo.BookingDue(ctx, s.pkgBooking))
	if facts.Collected != 200_000 {
		t.Fatalf("auto-match did not record the payment: collected=%d", facts.Collected)
	}

	// Receivables: agency, assignment, exposure, ageing.
	agency := must[*domain.Agency](t, "agency")(receivables.CreateAgency(ctx, s.branch, actor, app.AgencyInput{
		Code: "IT-AG", Name: "IT Agency", Currency: "SAR", CreditLimit: 2_000_000, TaxID: "1234567890",
	}))
	if err := receivables.AssignBooking(ctx, agency.ID, s.pkgBooking, actor); err != nil {
		t.Fatal(err)
	}
	rc := must[*app.Receivables](t, "receivables")(receivables.Receivables(ctx, &s.branch))
	var view *app.AgencyView
	for i := range rc.Agencies {
		if rc.Agencies[i].Agency.ID == agency.ID {
			view = &rc.Agencies[i]
		}
	}
	if view == nil || view.Exposure.Outstanding != 300_000 || view.Exposure.OpenBookings != 1 {
		t.Fatalf("agency exposure %+v", view)
	}
	if len(rc.Ageing) == 0 || len(rc.Debtors) == 0 {
		t.Fatalf("ageing/debtors empty: %+v", rc)
	}
	must[int](t, "sweep")(receivables.SweepOverdue(ctx))

	// Overview and profitability.
	o := must[*app.Overview](t, "overview")(overview.Overview(ctx, &s.branch))
	if o.Cash.Total == 0 || len(o.Trend) != 6 {
		t.Fatalf("overview %+v", o)
	}
	from, to := must2[time.Time, time.Time](t)(profit.Window("", ""))
	p := must[*app.Profitability](t, "profitability")(profit.Profitability(ctx, &s.branch, from, to))
	if len(p.Bookings) < 2 || len(p.Reps) == 0 || len(p.Departures) == 0 {
		t.Fatalf("profitability %+v", p)
	}
	must[*domain.Budget](t, "budget")(profit.SetBudget(ctx, s.departure, actor, app.BudgetInput{Currency: "SAR", Revenue: 900_000, Cost: 700_000}))
	must[app.Settings](t, "rates")(profit.SetRates(ctx, s.branch, actor, app.RatesInput{CommissionBPS: 1200}))

	// BSP reconciliation.
	st, lines := must2[*domain.BSPStatement, []domain.BSPLine](t)(recon.ImportStatement(ctx, s.branch, actor, app.StatementInput{
		Label: "IT", PeriodStart: today, PeriodEnd: today, Currency: "SAR",
		Lines: []app.StatementLine{
			{DocumentNo: "0651234567890", PNR: "QA7X9Z", Type: "sale", Amount: 101_000},
			{DocumentNo: "0651234567891", PNR: "ZZ1Y2X", Type: "sale", Amount: 50_000},
		},
	}))
	if st.Mismatched != 1 || st.MissingSystem != 1 || len(lines) < 2 {
		t.Fatalf("bsp %+v", st)
	}
	st2, lines2 := must2[*domain.BSPStatement, []domain.BSPLine](t)(recon.Statement(ctx, st.ID))
	if st2.ID != st.ID || len(lines2) != len(lines) {
		t.Fatal("statement reload")
	}

	// Balance confirmation letter round trip.
	letter, token := must2[*domain.Letter, string](t)(recon.CreateLetter(ctx, &s.branch, actor, app.LetterInput{PartyType: "agency", PartyID: agency.ID}))
	if letter.Balance != 300_000 {
		t.Fatalf("letter balance %d", letter.Balance)
	}
	pub := must[*app.PublicLetter](t, "public")(recon.LetterByToken(ctx, token))
	if pub.Letter.ID != letter.ID {
		t.Fatal("token lookup")
	}
	must[*app.PublicLetter](t, "respond")(recon.RespondLetter(ctx, token, app.LetterResponse{Confirm: true, Name: "IT"}))
	if _, err := recon.RespondLetter(ctx, token, app.LetterResponse{Confirm: false, Name: "IT"}); err == nil {
		t.Fatal("second response accepted")
	}
	q := must[*app.RefundQuote](t, "refund")(recon.QuoteRefund(ctx, app.RefundQuoteInput{BookingID: &s.pkgBooking, SupplierPenalty: 50_000, ServiceFee: 10_000}))
	if q.Input.Paid != 200_000 {
		t.Fatalf("refund quote %+v", q)
	}

	// Payables plan query.
	must[[]app.DueInvoice](t, "due invoices")(repo.DueInvoices(ctx, &s.branch, time.Now().AddDate(0, 0, 60)))
	fmt.Fprintln(os.Stderr, "finance hub integration: ok")
}

func must2[A, B any](t *testing.T) func(A, B, error) (A, B) {
	return func(a A, b B, err error) (A, B) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return a, b
	}
}
