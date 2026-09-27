package fx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type memRepo struct {
	rows map[uuid.UUID]domain.StoredRate
}

func newMemRepo() *memRepo { return &memRepo{rows: map[uuid.UUID]domain.StoredRate{}} }

func (m *memRepo) taken(r *domain.StoredRate) bool {
	for _, x := range m.rows {
		if x.Base == r.Base && x.Quote == r.Quote && x.EffectiveDate.Equal(r.EffectiveDate) {
			return true
		}
	}
	return false
}

func (m *memRepo) Insert(_ context.Context, r *domain.StoredRate) error {
	if m.taken(r) {
		return domain.ErrRateExists
	}
	m.rows[r.ID] = *r
	return nil
}

func (m *memRepo) InsertIfAbsent(ctx context.Context, r *domain.StoredRate) (bool, error) {
	if m.taken(r) {
		return false, nil
	}
	return true, m.Insert(ctx, r)
}

func (m *memRepo) UpdateRate(_ context.Context, id uuid.UUID, scaled int64, source string) error {
	r, ok := m.rows[id]
	if !ok {
		return shared.NewNotFound("fx rate")
	}
	r.Scaled, r.Source = scaled, source
	m.rows[id] = r
	return nil
}

func (m *memRepo) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.rows[id]; !ok {
		return shared.NewNotFound("fx rate")
	}
	delete(m.rows, id)
	return nil
}

func (m *memRepo) Get(_ context.Context, id uuid.UUID) (*domain.StoredRate, error) {
	r, ok := m.rows[id]
	if !ok {
		return nil, shared.NewNotFound("fx rate")
	}
	return &r, nil
}

func (m *memRepo) List(context.Context, domain.ListFilter) ([]domain.StoredRate, int64, error) {
	out := make([]domain.StoredRate, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, int64(len(out)), nil
}

func (m *memRepo) Latest(_ context.Context, base, quote string, on time.Time) (*domain.Rate, error) {
	var best *domain.Rate
	for _, r := range m.rows {
		if r.Base == base && r.Quote == quote && !r.EffectiveDate.After(on) && (best == nil || r.EffectiveDate.After(best.EffectiveDate)) {
			rate := r.Rate
			best = &rate
		}
	}
	return best, nil
}

type recorder struct {
	events []audit.RecordInput
	err    error
}

func (r *recorder) Record(_ context.Context, in audit.RecordInput) error {
	if r.err != nil {
		return r.err
	}
	r.events = append(r.events, in)
	return nil
}

func scoped() context.Context {
	return access.WithScope(context.Background(), access.Scope{Level: access.LevelBranch, UserID: uuid.New(), BranchID: uuid.New()})
}

func appCode(err error) string {
	var app *shared.AppError
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func day(s string) time.Time {
	d, _ := time.Parse(time.DateOnly, s)
	return d
}

func TestCreateValidatesAndAudits(t *testing.T) {
	repo, rec := newMemRepo(), &recorder{}
	svc := NewService(repo, tx.Nop{}, rec, Options{})
	actor := uuid.New()
	r, err := svc.Create(scoped(), CreateInput{Base: "usd", Quote: "sar", Rate: "3.75", EffectiveDate: "2026-09-27", ActorID: actor})
	if err != nil {
		t.Fatal(err)
	}
	if r.Base != "USD" || r.Quote != "SAR" || r.Scaled != 375_000_000 || r.Source != "manual" || *r.CreatedBy != actor {
		t.Fatalf("stored rate: %+v", r)
	}
	if len(rec.events) != 1 || rec.events[0].Action != "fx.rate_created" || rec.events[0].After.(map[string]any)["rate"] != "3.75000000" {
		t.Fatalf("audit: %+v", rec.events)
	}
	_, err = svc.Create(scoped(), CreateInput{Base: "USD", Quote: "SAR", Rate: "3.76", EffectiveDate: "2026-09-27"})
	if appCode(err) != "fx_rate_exists" || !errors.Is(err, shared.ErrConflict) {
		t.Fatalf("duplicate date must be 409 fx_rate_exists, got %v", err)
	}
	for _, in := range []CreateInput{
		{Base: "USD", Quote: "USD", Rate: "1", EffectiveDate: "2026-09-27"},
		{Base: "USD", Quote: "SAR", Rate: "0", EffectiveDate: "2026-09-28"},
		{Base: "USD", Quote: "SAR", Rate: "3.123456789", EffectiveDate: "2026-09-28"},
		{Base: "USD", Quote: "SAR", Rate: "3.75", EffectiveDate: "27/09/2026"},
	} {
		if _, err := svc.Create(scoped(), in); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%+v must be a validation error, got %v", in, err)
		}
	}
}

func TestMutationsFailClosedWhenAuditFails(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, tx.Nop{}, &recorder{err: errors.New("audit down")}, Options{})
	if _, err := svc.Create(scoped(), CreateInput{Base: "USD", Quote: "SAR", Rate: "3.75", EffectiveDate: "2026-09-27"}); err == nil {
		t.Fatal("create must fail when the audit write fails")
	}
}

func TestUpdateAndDeleteAuditBeforeAfter(t *testing.T) {
	repo, rec := newMemRepo(), &recorder{}
	svc := NewService(repo, tx.Nop{}, rec, Options{})
	r, _ := svc.Create(scoped(), CreateInput{Base: "USD", Quote: "SAR", Rate: "3.75", EffectiveDate: "2026-09-27"})
	src := "sama"
	up, err := svc.Update(scoped(), UpdateInput{ID: r.ID, Rate: "3.7512", Source: &src})
	if err != nil || up.Scaled != 375_120_000 || up.Source != "sama" {
		t.Fatalf("update: %+v %v", up, err)
	}
	ev := rec.events[1]
	if ev.Action != "fx.rate_updated" || ev.Before.(map[string]any)["rate"] != "3.75000000" || ev.After.(map[string]any)["rate"] != "3.75120000" {
		t.Fatalf("update audit: %+v", ev)
	}
	if err := svc.Delete(scoped(), r.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if ev := rec.events[2]; ev.Action != "fx.rate_deleted" || ev.Before == nil || ev.After != nil {
		t.Fatalf("delete audit: %+v", ev)
	}
	if err := svc.Delete(scoped(), r.ID, uuid.New()); !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("second delete must be 404, got %v", err)
	}
}

func TestMutationsRequireScope(t *testing.T) {
	svc := NewService(newMemRepo(), tx.Nop{}, &recorder{}, Options{})
	if _, err := svc.Create(context.Background(), CreateInput{Base: "USD", Quote: "SAR", Rate: "3.75", EffectiveDate: "2026-09-27"}); err == nil {
		t.Fatal("missing scope must fail closed")
	}
}

func TestConverterUsesLatestEffectiveAndInverse(t *testing.T) {
	repo := newMemRepo()
	ctx := scoped()
	svc := NewService(repo, tx.Nop{}, &recorder{}, Options{})
	for _, in := range []CreateInput{
		{Base: "USD", Quote: "SAR", Rate: "3.70", EffectiveDate: "2026-09-01"},
		{Base: "USD", Quote: "SAR", Rate: "3.75", EffectiveDate: "2026-09-20"},
	} {
		if _, err := svc.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	conv := svc.Converter()

	c, err := conv.Convert(ctx, 10000, "USD", "SAR", day("2026-09-19"))
	if err != nil || c.Amount != 37000 || !c.Rate.EffectiveDate.Equal(day("2026-09-01")) {
		t.Fatalf("rate effective before the 20th: %+v %v", c, err)
	}
	c, _ = conv.Convert(ctx, 10000, "usd", "sar", day("2026-09-27"))
	if c.Amount != 37500 || c.Rate.Scaled != 375_000_000 {
		t.Fatalf("latest rate: %+v", c)
	}
	c, err = conv.Convert(ctx, 37500, "SAR", "USD", day("2026-09-27"))
	if err != nil || c.Amount != 10000 || c.Rate.Scaled != 26_666_667 || c.Rate.Base != "SAR" || c.Rate.Quote != "USD" {
		t.Fatalf("inverse: %+v %v", c, err)
	}
	c, _ = conv.Convert(ctx, 1234, "SAR", "SAR", day("2026-01-01"))
	if c.Amount != 1234 || c.Rate.Scaled != domain.RateScale {
		t.Fatalf("identity: %+v", c)
	}
	if _, err := conv.Convert(ctx, 100, "USD", "SAR", day("2026-08-31")); !errors.Is(err, domain.ErrRateNotFound) {
		t.Fatalf("before the first rate: %v", err)
	}
	if _, err := svc.Convert(ctx, 100, "EUR", "SAR", nil); appCode(err) != "fx_rate_not_found" || !errors.Is(err, shared.ErrNotFound) {
		t.Fatalf("service must map to 404 fx_rate_not_found, got %v", err)
	}
}

type fakeProvider struct{ rates []domain.Rate }

func (fakeProvider) Name() string { return "fake" }
func (p fakeProvider) Fetch(context.Context, time.Time) ([]domain.Rate, error) {
	return p.rates, nil
}

func TestSyncRates(t *testing.T) {
	ctx := access.WithScope(context.Background(), access.System())
	noop := NewService(newMemRepo(), tx.Nop{}, &recorder{}, Options{})
	if n, err := noop.SyncRates(ctx); n != 0 || err != nil {
		t.Fatalf("sync without provider must be a no-op: %d %v", n, err)
	}

	repo, rec := newMemRepo(), &recorder{}
	manual := NewService(repo, tx.Nop{}, rec, Options{})
	if _, err := manual.Create(scoped(), CreateInput{Base: "USD", Quote: "SAR", Rate: "3.75", EffectiveDate: "2026-09-27"}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(repo, tx.Nop{}, rec, Options{Providers: []domain.RateProvider{fakeProvider{rates: []domain.Rate{
		{Base: "USD", Quote: "SAR", Scaled: 380_000_000, EffectiveDate: day("2026-09-27")},
		{Base: "USD", Quote: "EUR", Scaled: 92_000_000, EffectiveDate: day("2026-09-27")},
		{Base: "USD", Quote: "USD", Scaled: domain.RateScale},
	}}}})
	n, err := svc.SyncRates(ctx)
	if err != nil || n != 1 {
		t.Fatalf("sync inserted %d, %v; want 1 (manual rate kept, invalid skipped)", n, err)
	}
	c, _ := svc.Converter().Convert(ctx, 10000, "USD", "SAR", day("2026-09-27"))
	if c.Amount != 37500 {
		t.Fatalf("provider must not overwrite a manual rate: %+v", c)
	}
	if last := rec.events[len(rec.events)-1]; last.Action != "fx.rate_created" || last.Extra["provider"] != "fake" {
		t.Fatalf("sync audit: %+v", last)
	}
}
