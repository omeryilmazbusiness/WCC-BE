package supplier

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct {
	domain.Repository
	suppliers map[uuid.UUID]domain.Supplier
	creds     map[uuid.UUID]string
	ledger    []domain.LedgerEntry
	usage     []domain.Usage
	disputes  map[uuid.UUID]domain.Dispute
}

func newMemRepo() *memRepo {
	return &memRepo{suppliers: map[uuid.UUID]domain.Supplier{}, creds: map[uuid.UUID]string{},
		disputes: map[uuid.UUID]domain.Dispute{}}
}

func (m *memRepo) Create(_ context.Context, s *domain.Supplier) error {
	for _, o := range m.suppliers {
		if o.BranchID == s.BranchID && o.Code == s.Code {
			return shared.NewConflict("supplier code already exists")
		}
	}
	m.suppliers[s.ID] = *s
	return nil
}
func (m *memRepo) Update(_ context.Context, s *domain.Supplier) error {
	m.suppliers[s.ID] = *s
	return nil
}
func (m *memRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Supplier, error) {
	s, ok := m.suppliers[id]
	if !ok {
		return nil, errors.New("no rows")
	}
	return &s, nil
}
func (m *memRepo) FindForUpdate(_ context.Context, id uuid.UUID) (*domain.Supplier, error) {
	s, ok := m.suppliers[id]
	if !ok {
		return nil, shared.NewNotFound("supplier")
	}
	return &s, nil
}
func (m *memRepo) List(_ context.Context, _ *uuid.UUID, _ bool) ([]domain.Supplier, error) {
	out := []domain.Supplier{}
	for _, s := range m.suppliers {
		out = append(out, s)
	}
	return out, nil
}
func (m *memRepo) LoadCredentials(_ context.Context, id uuid.UUID) (string, error) {
	return m.creds[id], nil
}
func (m *memRepo) StoreCredentials(_ context.Context, id uuid.UUID, sealed string) error {
	m.creds[id] = sealed
	return nil
}
func (m *memRepo) ListContractsEnding(_ context.Context, _ *uuid.UUID, from, to time.Time) ([]domain.Supplier, error) {
	out := []domain.Supplier{}
	for _, s := range m.suppliers {
		if s.IsActive && s.ContractEnd != nil && !s.ContractEnd.Before(from) && !s.ContractEnd.After(to) {
			out = append(out, s)
		}
	}
	return out, nil
}
func (m *memRepo) CreateLedgerEntry(_ context.Context, e *domain.LedgerEntry) error {
	m.ledger = append(m.ledger, *e)
	return nil
}
func (m *memRepo) ListLedger(context.Context, uuid.UUID, int) ([]domain.LedgerEntry, error) {
	return m.ledger, nil
}
func (m *memRepo) VolumeSince(context.Context, uuid.UUID, time.Time) (domain.Volume, error) {
	return domain.Volume{}, nil
}
func (m *memRepo) AddUsage(_ context.Context, u *domain.Usage) error {
	m.usage = append(m.usage, *u)
	return nil
}
func (m *memRepo) MetricsSince(context.Context, uuid.UUID, time.Time) (domain.Metrics, error) {
	var out domain.Metrics
	for _, u := range m.usage {
		out.Searches += int64(u.Searches)
		out.Bookings += int64(u.Bookings)
	}
	return out, nil
}
func (m *memRepo) CreateDispute(_ context.Context, d *domain.Dispute) error {
	m.disputes[d.ID] = *d
	return nil
}
func (m *memRepo) UpdateDispute(_ context.Context, d *domain.Dispute) error {
	m.disputes[d.ID] = *d
	return nil
}
func (m *memRepo) FindDispute(_ context.Context, id uuid.UUID) (*domain.Dispute, error) {
	d, ok := m.disputes[id]
	if !ok {
		return nil, shared.NewNotFound("dispute")
	}
	return &d, nil
}
func (m *memRepo) ListDisputes(context.Context, uuid.UUID) ([]domain.Dispute, error) {
	out := []domain.Dispute{}
	for _, d := range m.disputes {
		out = append(out, d)
	}
	return out, nil
}

type fakeAlerts struct {
	low      []string
	expiring map[string]int
}

func (f *fakeAlerts) SupplierLowBalance(_ context.Context, s *domain.Supplier) error {
	f.low = append(f.low, s.Code)
	return nil
}
func (f *fakeAlerts) SupplierContractExpiring(_ context.Context, s *domain.Supplier, days int) error {
	f.expiring[s.Code] = days
	return nil
}

type fakeProber struct {
	latency time.Duration
	code    int
	err     error
	urls    []string
}

func (p *fakeProber) Probe(_ context.Context, url string) (time.Duration, int, error) {
	p.urls = append(p.urls, url)
	return p.latency, p.code, p.err
}

type auditLog struct{ actions []string }

func (a *auditLog) Record(_ context.Context, in audit.RecordInput) error {
	a.actions = append(a.actions, in.Action)
	return nil
}

var branch = uuid.MustParse("11111111-1111-1111-1111-111111111111")

func newTestService(t *testing.T) (*Service, *memRepo, *fakeAlerts, *auditLog) {
	t.Helper()
	kr, err := crypto.NewKeyring("k1", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := newMemRepo()
	alerts := &fakeAlerts{expiring: map[string]int{}}
	log := &auditLog{}
	svc := NewService(repo, tx.Nop{})
	svc.SetSecrets(crypto.NewSecretBox(kr))
	svc.SetFundsAlerts(alerts)
	svc.SetAuditor(log)
	svc.now = func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC) }
	return svc, repo, alerts, log
}

func duffel() ProfileInput {
	return ProfileInput{
		BranchID: branch, Code: "SUP-DUFFEL-01", NameEn: "Duffel Financial Ltd", Category: domain.CategoryGDS,
		ContactEmail: "am@duffel.com", IntegrationType: domain.IntegrationAPI, Environment: domain.EnvSandbox,
		APIBaseURL: "https://api.duffel.com", PaymentModel: domain.PaymentPrepaid, Currency: "SAR",
		LowBalanceThreshold: 5_000_00, Markups: map[string]int{"flight": 300},
		Credentials: map[string]string{"api_key": "duffel_test_abcd1234", "client_id": "client-9876"},
	}
}

func TestCreateSealsCredentialsAndReturnsHintsOnly(t *testing.T) {
	svc, repo, _, log := newTestService(t)
	ctx := context.Background()
	sup, err := svc.Create(ctx, duffel())
	if err != nil {
		t.Fatal(err)
	}
	sealed := repo.creds[sup.ID]
	if sealed == "" || strings.Contains(sealed, "abcd1234") {
		t.Fatalf("credentials must be stored sealed, got %q", sealed)
	}
	d, err := svc.Detail(ctx, sup.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if d.CredentialHints["api_key"] != "••••1234" || d.CredentialHints["client_id"] != "••••9876" {
		t.Fatalf("hints = %v", d.CredentialHints)
	}
	if len(d.Ledger) != 0 {
		t.Fatal("ledger must be hidden without the finance permission")
	}
	if d.Availability.Bookable || d.Availability.Reason != domain.BlockDepositEmpty {
		t.Fatalf("a fresh prepaid supplier is closed to search: %+v", d.Availability)
	}
	if _, err := svc.Create(ctx, duffel()); err == nil {
		t.Fatal("duplicate codes must be rejected")
	}
	if len(log.actions) != 1 || log.actions[0] != "supplier.created" {
		t.Fatalf("audit = %v", log.actions)
	}
}

func TestUpdatePatchesCredentialsAndGuardsBalance(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	ctx := context.Background()
	sup, _ := svc.Create(ctx, duffel())

	in := duffel()
	in.Code = "CHANGED"
	in.Credentials = map[string]string{"api_key": "", "account_id": "acct-5555"}
	updated, err := svc.Update(ctx, sup.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Code != "SUP-DUFFEL-01" {
		t.Fatal("code must stay fixed after creation")
	}
	hints, _ := svc.CredentialHints(ctx, sup.ID)
	if _, ok := hints["api_key"]; ok || hints["client_id"] != "••••9876" || hints["account_id"] != "••••5555" {
		t.Fatalf("credential patch = %v", hints)
	}

	if _, _, err := svc.PostLedger(ctx, LedgerInput{SupplierID: sup.ID, Kind: domain.EntryTopUp, Amount: 12_450_00}); err != nil {
		t.Fatal(err)
	}
	in.Credentials = nil
	in.PaymentModel = domain.PaymentPostpaid
	if _, err := svc.Update(ctx, sup.ID, in); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("payment model change with a balance must fail, got %v", err)
	}
	if repo.suppliers[sup.ID].Finance.Model != domain.PaymentPrepaid {
		t.Fatal("rejected update must not be stored")
	}
}

func TestPostLedgerAlertsOnceWhenCrossingThreshold(t *testing.T) {
	svc, repo, alerts, log := newTestService(t)
	ctx := context.Background()
	sup, _ := svc.Create(ctx, duffel())
	actor := uuid.New()

	if _, _, err := svc.PostLedger(ctx, LedgerInput{SupplierID: sup.ID, ActorID: actor, Kind: domain.EntryTopUp, Amount: 12_450_00}); err != nil {
		t.Fatal(err)
	}
	if len(alerts.low) != 0 {
		t.Fatalf("top-up above threshold must not alert: %v", alerts.low)
	}
	entry, after, err := svc.PostLedger(ctx, LedgerInput{SupplierID: sup.ID, Kind: domain.EntryCharge, Amount: 8_000_00, Reference: "BKG-1"})
	if err != nil {
		t.Fatal(err)
	}
	if entry.BalanceAfter != 4_450_00 || after.Finance.DepositBalance != 4_450_00 || entry.Currency != "SAR" {
		t.Fatalf("entry = %+v", entry)
	}
	if len(alerts.low) != 1 {
		t.Fatalf("crossing the threshold must alert once, got %v", alerts.low)
	}
	if _, _, err := svc.PostLedger(ctx, LedgerInput{SupplierID: sup.ID, Kind: domain.EntryCharge, Amount: 100_00}); err != nil {
		t.Fatal(err)
	}
	if len(alerts.low) != 1 {
		t.Fatal("staying below the threshold must not alert again")
	}
	if _, _, err := svc.PostLedger(ctx, LedgerInput{SupplierID: sup.ID, Kind: domain.EntryCharge, Amount: 4_350_00}); err != nil {
		t.Fatal(err)
	}
	if len(alerts.low) != 2 {
		t.Fatal("exhausting the deposit must alert again (closed to search)")
	}
	if _, _, err := svc.PostLedger(ctx, LedgerInput{SupplierID: sup.ID, Kind: domain.EntryCharge, Amount: 1_000_000_00}); !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("overdraft must conflict, got %v", err)
	}
	if _, _, err := svc.PostLedger(ctx, LedgerInput{SupplierID: sup.ID, Kind: domain.EntryAdjustment, Amount: -1}); err == nil {
		t.Fatal("adjustments need a reason")
	}
	if len(repo.ledger) != 4 || *repo.ledger[0].ActorID != actor {
		t.Fatalf("ledger = %+v", repo.ledger)
	}
	if n := strings.Count(strings.Join(log.actions, ","), "supplier.ledger_posted"); n != 4 {
		t.Fatalf("ledger audit count = %d", n)
	}
}

func TestHealthCheckUsesProber(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()
	in := duffel()
	in.APIBaseURL = ""
	sup, _ := svc.Create(ctx, in)
	p := &fakeProber{latency: 320 * time.Millisecond, code: 401}
	svc.SetProber(p)
	if _, err := svc.HealthCheck(ctx, sup.ID); !errors.Is(err, shared.ErrInvalidState) {
		t.Fatalf("health check without a URL must fail, got %v", err)
	}
	in = duffel()
	if _, err := svc.Update(ctx, sup.ID, in); err != nil {
		t.Fatal(err)
	}
	got, err := svc.HealthCheck(ctx, sup.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Health.Status != domain.HealthActive || got.Health.LatencyMs != 320 || got.Health.Note != "HTTP 401" {
		t.Fatalf("health = %+v", got.Health)
	}
	p.err = errors.New("connection failed")
	got, _ = svc.HealthCheck(ctx, sup.ID)
	if got.Health.Status != domain.HealthDown || got.AvailabilityOn(svc.Today()).Reason == "" {
		t.Fatalf("a down API must close the supplier: %+v", got.Health)
	}
	got, err = svc.SetHealth(ctx, sup.ID, domain.HealthDegraded, "Planned maintenance")
	if err != nil || got.Health.Status != domain.HealthDegraded {
		t.Fatalf("manual health = %+v %v", got.Health, err)
	}
}

func TestRoutingAndUsage(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ctx := context.Background()
	a, _ := svc.Create(ctx, duffel())
	b := duffel()
	b.Code, b.PaymentModel = "SUP-AMADEUS", domain.PaymentCard
	bs, _ := svc.Create(ctx, b)
	hotel := duffel()
	hotel.Code, hotel.Category = "SUP-RATEHAWK", domain.CategoryWholesaler
	_, _ = svc.Create(ctx, hotel)

	opts, err := svc.Routing(ctx, &branch, domain.ProductFlight)
	if err != nil {
		t.Fatal(err)
	}
	if len(opts) != 2 || opts[0].Supplier.ID != bs.ID || opts[1].Supplier.ID != a.ID || opts[1].Availability.Bookable {
		t.Fatalf("routing = %+v", opts)
	}
	if _, err := svc.Routing(ctx, &branch, "cruise"); err == nil {
		t.Fatal("unknown products must fail")
	}
	m, err := svc.RecordUsage(ctx, domain.Usage{SupplierID: a.ID, Searches: 1000, Bookings: 8})
	if err != nil || m.BookingsPer1000() != 8 {
		t.Fatalf("usage = %+v %v", m, err)
	}
	if _, err := svc.RecordUsage(ctx, domain.Usage{SupplierID: a.ID}); err == nil {
		t.Fatal("empty usage must fail")
	}
}

func TestDisputesAndContractSweep(t *testing.T) {
	svc, _, alerts, _ := newTestService(t)
	ctx := context.Background()
	in := duffel()
	in.ContractEnd = "2026-10-20"
	sup, _ := svc.Create(ctx, in)
	other := duffel()
	other.Code, other.ContractEnd = "SUP-LATER", "2027-06-01"
	_, _ = svc.Create(ctx, other)

	n, err := svc.ContractExpirySweep(ctx, nil)
	if err != nil || n != 1 || alerts.expiring["SUP-DUFFEL-01"] != 15 {
		t.Fatalf("sweep = %d %v %v", n, err, alerts.expiring)
	}

	d, err := svc.OpenDispute(ctx, DisputeInput{SupplierID: sup.ID, Title: "Price changed after ticketing", Amount: 320_00})
	if err != nil || d.Currency != "SAR" || d.Status != domain.DisputeOpen {
		t.Fatalf("dispute = %+v %v", d, err)
	}
	if _, err := svc.CloseDispute(ctx, uuid.New(), d.ID, domain.DisputeResolved, ""); err == nil {
		t.Fatal("closing through another supplier must fail")
	}
	closed, err := svc.CloseDispute(ctx, sup.ID, d.ID, domain.DisputeResolved, "Refund received")
	if err != nil || closed.Status != domain.DisputeResolved || closed.ResolvedAt == nil {
		t.Fatalf("close = %+v %v", closed, err)
	}
	if _, err := svc.Create(ctx, ProfileInput{BranchID: branch, Code: "SUP-X", NameEn: "X", ContractEnd: "20-10-2026"}); err == nil {
		t.Fatal("bad dates must be rejected")
	}
}
