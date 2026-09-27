package fx

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memSnapshots struct {
	mu   sync.Mutex
	rows map[string]domain.SourceSnapshot
}

func newMemSnapshots() *memSnapshots { return &memSnapshots{rows: map[string]domain.SourceSnapshot{}} }

func snapKey(source string, kind domain.SourceKind) string { return source + "/" + string(kind) }

func (m *memSnapshots) List(context.Context) ([]domain.SourceSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.SourceSnapshot, 0, len(m.rows))
	for _, s := range m.rows {
		out = append(out, s)
	}
	return out, nil
}

func (m *memSnapshots) SaveSuccess(_ context.Context, s domain.SourceSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.OK, s.Error, s.AttemptedAt = true, "", s.FetchedAt
	m.rows[snapKey(s.Source, s.Kind)] = s
	return nil
}

func (m *memSnapshots) SaveFailure(_ context.Context, source string, kind domain.SourceKind, msg string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.rows[snapKey(source, kind)]
	s.Source, s.Kind, s.OK, s.Error, s.AttemptedAt = source, kind, false, msg, at
	m.rows[snapKey(source, kind)] = s
	return nil
}

func (m *memSnapshots) get(source string, kind domain.SourceKind) domain.SourceSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rows[snapKey(source, kind)]
}

type fakeSource struct {
	info  domain.LiveSourceInfo
	mu    sync.Mutex
	calls int
	fetch func() ([]domain.SourceSnapshot, error)
}

func (f *fakeSource) Info() domain.LiveSourceInfo { return f.info }

func (f *fakeSource) Fetch(context.Context) ([]domain.SourceSnapshot, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.fetch()
}

func (f *fakeSource) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func dec(s string) int64 {
	v, err := domain.ParseDecimal(s)
	if err != nil {
		panic(err)
	}
	return v
}

var (
	obsFresh = time.Date(2026, 9, 27, 7, 58, 31, 0, time.UTC)
	obsRef   = time.Date(2026, 9, 27, 0, 2, 31, 0, time.UTC)
	syncAt   = time.Date(2026, 9, 27, 10, 25, 0, 0, time.UTC)
)

func liraSnapshots(usdOfficialMid string) []domain.SourceSnapshot {
	return []domain.SourceSnapshot{
		{Source: "lirascope", Kind: domain.KindOfficial, OK: true, Quotes: []domain.Quote{
			{Currency: "USD", Buy: dec("121.5"), Sell: dec("122.5"), Mid: dec(usdOfficialMid), ObservedAt: obsFresh},
		}},
		{Source: "lirascope", Kind: domain.KindMarket, OK: true, Quotes: []domain.Quote{
			{Currency: "USD", Buy: dec("137"), Sell: dec("137.75"), Mid: dec("137.375"), ObservedAt: obsFresh},
			{Currency: "EUR", Buy: dec("154.8"), Sell: dec("156.9"), Mid: dec("155.85"), ObservedAt: obsFresh},
			{Currency: "SAR", Buy: dec("36.14"), Sell: dec("36.71"), Mid: dec("36.425"), ObservedAt: obsFresh},
		}},
	}
}

func refSnapshots(next time.Time) []domain.SourceSnapshot {
	return []domain.SourceSnapshot{{Source: "exchangerate-api", Kind: domain.KindReference, OK: true, NextUpdateAt: next, Quotes: []domain.Quote{
		{Currency: "USD", Mid: domain.RateScale, ObservedAt: obsRef},
		{Currency: "SYP", Mid: dec("121.843708"), ObservedAt: obsRef},
		{Currency: "EUR", Mid: dec("0.877506"), ObservedAt: obsRef},
		{Currency: "SAR", Mid: dec("3.75"), ObservedAt: obsRef},
	}}}
}

type liveFixture struct {
	svc   *LiveService
	snaps *memSnapshots
	rates *memRepo
	rec   *recorder
	lira  *fakeSource
	ref   *fakeSource
	now   time.Time
}

func newLiveFixture() *liveFixture {
	f := &liveFixture{snaps: newMemSnapshots(), rates: newMemRepo(), rec: &recorder{}, now: syncAt}
	f.lira = &fakeSource{
		info:  domain.LiveSourceInfo{ID: "lirascope", Name: "LiraScope", Kinds: []domain.SourceKind{domain.KindOfficial, domain.KindMarket}, MinInterval: 5 * time.Minute},
		fetch: func() ([]domain.SourceSnapshot, error) { return liraSnapshots("122"), nil },
	}
	f.ref = &fakeSource{
		info: domain.LiveSourceInfo{ID: "exchangerate-api", Name: "ExchangeRate-API", Attribution: "Rates By Exchange Rate API",
			Kinds: []domain.SourceKind{domain.KindReference}, MinInterval: 6 * time.Hour},
		fetch: func() ([]domain.SourceSnapshot, error) { return refSnapshots(syncAt.Add(14 * time.Hour)), nil },
	}
	f.svc = NewLiveService([]domain.LiveSource{f.lira, f.ref}, f.snaps, f.rates, tx.Nop{}, f.rec, LiveOptions{
		Board: domain.BoardConfig{
			Local: "SYP", Pinned: []string{"USD", "EUR", "SAR"}, Currencies: []string{"USD", "EUR", "SAR", "KWD"},
			MarketMaxAge: 48 * time.Hour,
		},
		Disclaimer: "indicative",
	})
	f.svc.now = func() time.Time { return f.now }
	return f
}

func boardQuote(t *testing.T, b *LiveBoard, cur string) domain.BoardQuote {
	t.Helper()
	for _, q := range b.Quotes {
		if q.Currency == cur {
			return q
		}
	}
	t.Fatalf("no quote %s", cur)
	return domain.BoardQuote{}
}

func TestLiveSyncIsolatesSourceFailures(t *testing.T) {
	f := newLiveFixture()
	f.ref.fetch = func() ([]domain.SourceSnapshot, error) { return nil, errors.New("status 429") }
	if err := f.svc.Sync(context.Background(), false); err != nil {
		t.Fatalf("a failing provider must not fail the sync: %v", err)
	}
	if s := f.snaps.get("lirascope", domain.KindOfficial); !s.OK || !s.FetchedAt.Equal(syncAt) || len(s.Quotes) != 1 {
		t.Errorf("lirascope official: %+v", s)
	}
	if s := f.snaps.get("exchangerate-api", domain.KindReference); s.OK || s.Error != "status 429" || !s.FetchedAt.IsZero() {
		t.Errorf("exchangerate-api: %+v", s)
	}

	b, err := f.svc.Board(scoped())
	if err != nil {
		t.Fatal(err)
	}
	if b.Stale || !b.UpdatedAt.Equal(syncAt) || b.Disclaimer != "indicative" {
		t.Errorf("board header: %+v", b.Board)
	}
	if eur := boardQuote(t, b, "EUR"); eur.Official != nil || eur.Market == nil || eur.USDCross != 0 {
		t.Errorf("without reference EUR official cannot be derived: %+v", eur)
	}
	if !b.Sources[0].OK || b.Sources[1].OK || b.Sources[1].Error != "status 429" || !b.Sources[1].FetchedAt.IsZero() {
		t.Errorf("sources: %+v", b.Sources)
	}
}

func TestLiveSyncKeepsLastGoodPayloadOnFailure(t *testing.T) {
	f := newLiveFixture()
	if err := f.svc.Sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	f.now = syncAt.Add(10 * time.Minute)
	f.lira.fetch = func() ([]domain.SourceSnapshot, error) { return liraSnapshots("12200"), nil }
	if err := f.svc.Sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	s := f.snaps.get("lirascope", domain.KindMarket)
	if s.OK || !strings.Contains(s.Error, "old") || !s.FetchedAt.Equal(syncAt) || len(s.Quotes) != 3 {
		t.Fatalf("old-lira payload must be rejected as a whole, previous quotes kept: %+v", s)
	}
	if o := f.snaps.get("lirascope", domain.KindOfficial); o.Quotes[0].Mid != dec("122") {
		t.Fatalf("official USD must stay 122, got %d", o.Quotes[0].Mid)
	}
	b, err := f.svc.Board(scoped())
	if err != nil {
		t.Fatal(err)
	}
	if usd := boardQuote(t, b, "USD"); usd.Official == nil || usd.Official.Mid != dec("122") {
		t.Errorf("board keeps serving the last good USD: %+v", usd)
	}
	if b.Sources[0].OK || b.Sources[0].Error == "" || !b.Sources[0].FetchedAt.Equal(syncAt) {
		t.Errorf("lirascope status: %+v", b.Sources[0])
	}
}

func TestLiveSyncRespectsIntervals(t *testing.T) {
	f := newLiveFixture()
	ctx := context.Background()
	if err := f.svc.Sync(ctx, false); err != nil {
		t.Fatal(err)
	}
	f.now = syncAt.Add(2 * time.Minute)
	_ = f.svc.Sync(ctx, false)
	if f.lira.callCount() != 1 || f.ref.callCount() != 1 {
		t.Fatalf("within MinInterval nothing is fetched: lira=%d ref=%d", f.lira.callCount(), f.ref.callCount())
	}
	_ = f.svc.Sync(ctx, true)
	if f.lira.callCount() != 2 || f.ref.callCount() != 1 {
		t.Fatalf("force refetches LiraScope but never before the reference's next update: lira=%d ref=%d",
			f.lira.callCount(), f.ref.callCount())
	}
	f.now = f.now.Add(10 * time.Second)
	_ = f.svc.Sync(ctx, true)
	if f.lira.callCount() != 2 {
		t.Fatalf("force is floored at %s: lira=%d", forceFloor, f.lira.callCount())
	}
	f.now = syncAt.Add(15 * time.Hour)
	_ = f.svc.Sync(ctx, false)
	if f.lira.callCount() != 3 || f.ref.callCount() != 2 {
		t.Fatalf("after next update and MinInterval both refetch: lira=%d ref=%d", f.lira.callCount(), f.ref.callCount())
	}
}

func TestLiveBoardIsCached(t *testing.T) {
	f := newLiveFixture()
	ctx := context.Background()
	if err := f.svc.Sync(ctx, false); err != nil {
		t.Fatal(err)
	}
	first, err := f.svc.Board(scoped())
	if err != nil {
		t.Fatal(err)
	}
	_ = f.snaps.SaveFailure(ctx, "lirascope", domain.KindOfficial, "boom", f.now)
	if again, _ := f.svc.Board(scoped()); again != first {
		t.Error("board must be served from cache within the TTL")
	}
	f.now = f.now.Add(boardCacheTTL)
	if later, _ := f.svc.Board(scoped()); later == first || later.Sources[0].Error != "boom" {
		t.Error("board must be recomposed after the TTL")
	}
	if _, err := f.svc.Board(context.Background()); err == nil {
		t.Error("board requires an access scope")
	}
}

func TestLiveBoardBootstrapsEmptyStoreOnce(t *testing.T) {
	f := newLiveFixture()
	b, err := f.svc.Board(scoped())
	if err != nil {
		t.Fatal(err)
	}
	if f.lira.callCount() != 1 || f.ref.callCount() != 1 {
		t.Fatalf("an empty store must be synced on first read: lira=%d ref=%d", f.lira.callCount(), f.ref.callCount())
	}
	if b.Stale || len(b.Quotes) == 0 || boardQuote(t, b, "USD").Market == nil {
		t.Fatalf("first board must carry live quotes: %+v", b.Board)
	}
	f.now = f.now.Add(time.Hour)
	_ = f.snaps.SaveFailure(context.Background(), "lirascope", domain.KindOfficial, "boom", f.now)
	if _, err := f.svc.Board(scoped()); err != nil {
		t.Fatal(err)
	}
	if f.lira.callCount() != 1 {
		t.Fatalf("reads after bootstrap never fetch; that is the worker's job: lira=%d", f.lira.callCount())
	}
}

func TestLiveAdopt(t *testing.T) {
	f := newLiveFixture()
	if err := f.svc.Sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	r, err := f.svc.Adopt(scoped(), AdoptInput{Currency: "eur", Kind: "official", Side: "mid", ActorID: actor})
	if err != nil {
		t.Fatal(err)
	}
	if r.Base != "EUR" || r.Quote != "SYP" || domain.FormatRate(r.Scaled) != "139.03038840" ||
		r.Source != "derived:official:mid" || !r.EffectiveDate.Equal(day("2026-09-27")) || *r.CreatedBy != actor {
		t.Fatalf("adopted rate: %+v", r)
	}
	if len(f.rec.events) != 1 || f.rec.events[0].Action != "fx.rate_adopted" || f.rec.events[0].Extra["derived"] != true {
		t.Fatalf("audit: %+v", f.rec.events)
	}

	r, err = f.svc.Adopt(scoped(), AdoptInput{Currency: "USD", Kind: "market", Side: "sell", EffectiveDate: "2026-09-26"})
	if err != nil || r.Source != "lirascope:market:sell" || domain.FormatRate(r.Scaled) != "137.75000000" {
		t.Fatalf("market sell: %+v %v", r, err)
	}

	_, err = f.svc.Adopt(scoped(), AdoptInput{Currency: "EUR", Kind: "official", Side: "buy"})
	if appCode(err) != "fx_rate_exists" {
		t.Errorf("same pair and date: %v", err)
	}
	_, err = f.svc.Adopt(scoped(), AdoptInput{Currency: "KWD", Kind: "market", Side: "mid"})
	if appCode(err) != "live_quote_unavailable" || !errors.Is(err, shared.ErrInvalidState) {
		t.Errorf("null quote must be 422 live_quote_unavailable: %v", err)
	}
	for _, in := range []AdoptInput{
		{Currency: "SYP", Kind: "official", Side: "mid"},
		{Currency: "USD", Kind: "reference", Side: "mid"},
		{Currency: "USD", Kind: "official", Side: "average"},
		{Currency: "USD", Kind: "official", Side: "mid", EffectiveDate: "27.09.2026"},
	} {
		if _, err := f.svc.Adopt(scoped(), in); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%+v: want validation error, got %v", in, err)
		}
	}
}

func TestLiveAdoptFailsClosedOnAudit(t *testing.T) {
	f := newLiveFixture()
	if err := f.svc.Sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	f.rec.err = errors.New("audit down")
	if _, err := f.svc.Adopt(scoped(), AdoptInput{Currency: "USD", Kind: "official", Side: "mid"}); err == nil {
		t.Fatal("adopt must fail when the audit row cannot be written")
	}
}

func TestAccountingProviderWritesPinnedMids(t *testing.T) {
	f := newLiveFixture()
	if err := f.svc.Sync(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	fxSvc := NewService(f.rates, tx.Nop{}, f.rec, Options{Providers: []domain.RateProvider{
		fakeFailingProvider{}, f.svc.AccountingProvider(domain.KindOfficial),
	}})
	fxSvc.now = func() time.Time { return f.now }
	f.rates.rows[uuid.New()] = domain.StoredRate{Rate: domain.Rate{Base: "USD", Quote: "SYP", Scaled: dec("120"), EffectiveDate: day("2026-09-27"), Source: "manual"}}

	n, err := fxSvc.SyncRates(context.Background())
	if n != 2 || err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("inserted %d, err %v; want EUR+SAR inserted despite the failing provider", n, err)
	}
	got := map[string]domain.StoredRate{}
	for _, r := range f.rates.rows {
		got[r.Base] = r
	}
	if got["USD"].Source != "manual" || got["USD"].Scaled != dec("120") {
		t.Errorf("manual USD rate must not be overwritten: %+v", got["USD"])
	}
	if got["EUR"].Source != "derived:official" || domain.FormatRate(got["EUR"].Scaled) != "139.03038840" || got["EUR"].Quote != "SYP" {
		t.Errorf("EUR: %+v", got["EUR"])
	}
	if got["SAR"].Source != "derived:official" || domain.FormatRate(got["SAR"].Scaled) != "32.53333333" {
		t.Errorf("SAR: %+v", got["SAR"])
	}

	market := f.svc.AccountingProvider(domain.KindMarket)
	rates, err := market.Fetch(context.Background(), day("2026-09-27"))
	if err != nil || len(rates) != 3 || market.Name() != "lirascope:market" || rates[0].Source != "lirascope:market" {
		t.Fatalf("market rates: %+v %v", rates, err)
	}

	f.now = syncAt.Add(2 * time.Hour)
	if _, err := market.Fetch(context.Background(), day("2026-09-27")); err == nil {
		t.Fatal("a stale board must not feed accounting")
	}
}

type fakeFailingProvider struct{}

func (fakeFailingProvider) Name() string { return "generic" }
func (fakeFailingProvider) Fetch(context.Context, time.Time) ([]domain.Rate, error) {
	return nil, errors.New("provider down")
}
