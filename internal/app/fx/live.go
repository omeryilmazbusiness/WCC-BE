package fx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

const (
	boardCacheTTL   = 30 * time.Second
	boardStaleAfter = time.Hour
	// forceFloor keeps repeated manual refreshes from hammering a provider.
	forceFloor  = 30 * time.Second
	maxErrorLen = 300
)

type LiveOptions struct {
	// Board.StaleAfter defaults to one hour.
	Board      domain.BoardConfig
	Disclaimer string
	Location   *time.Location
	Log        *slog.Logger
}

// LiveService keeps the live rate board: it syncs sources into snapshots,
// composes the board and lets finance adopt a live quote as an accounting rate.
type LiveService struct {
	sources    []domain.LiveSource
	snaps      domain.SnapshotRepository
	rates      domain.Repository
	tx         tx.Runner
	audit      audit.Recorder
	cfg        domain.BoardConfig
	disclaimer string
	loc        *time.Location
	log        *slog.Logger
	now        func() time.Time

	syncMu       sync.Mutex
	cacheMu      sync.Mutex
	cached       *LiveBoard
	cachedAt     time.Time
	bootstrapped atomic.Bool
}

func NewLiveService(sources []domain.LiveSource, snaps domain.SnapshotRepository, rates domain.Repository,
	txm tx.Runner, auditor audit.Recorder, opts LiveOptions) *LiveService {
	cfg := opts.Board
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = boardStaleAfter
	}
	loc := opts.Location
	if loc == nil {
		loc = time.UTC
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &LiveService{
		sources: sources, snaps: snaps, rates: rates, tx: txm, audit: auditor, cfg: cfg,
		disclaimer: opts.Disclaimer, loc: loc, log: log, now: time.Now,
	}
}

// SourceStatus is the health of one source across its kinds.
type SourceStatus struct {
	Info domain.LiveSourceInfo
	OK   bool
	// FetchedAt is the last successful fetch; zero if none.
	FetchedAt time.Time
	Error     string
}

type LiveBoard struct {
	domain.Board
	Sources    []SourceStatus
	Disclaimer string
}

type AdoptInput struct {
	Currency      string
	Kind          string
	Side          string
	EffectiveDate string
	ActorID       uuid.UUID
}

// Sync fetches every source whose interval has elapsed (force shortens it to
// forceFloor). Provider failures are stored per source and logged; only
// persistence errors are returned.
func (s *LiveService) Sync(ctx context.Context, force bool) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	defer s.invalidate()

	stored, err := s.snaps.List(ctx)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	var due []domain.LiveSource
	for _, src := range s.sources {
		if isDue(src.Info(), stored, now, force) {
			due = append(due, src)
		}
	}
	type result struct {
		snaps []domain.SourceSnapshot
		err   error
	}
	results := make([]result, len(due))
	var wg sync.WaitGroup
	for i, src := range due {
		wg.Go(func() {
			snaps, err := src.Fetch(ctx)
			results[i] = result{snaps: snaps, err: err}
		})
	}
	wg.Wait()

	var errs []error
	for i, src := range due {
		if err := s.store(ctx, src.Info(), results[i].snaps, results[i].err, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func isDue(info domain.LiveSourceInfo, stored []domain.SourceSnapshot, now time.Time, force bool) bool {
	var attempted, next time.Time
	for _, sn := range stored {
		if sn.Source != info.ID {
			continue
		}
		if sn.AttemptedAt.After(attempted) {
			attempted = sn.AttemptedAt
		}
		if sn.NextUpdateAt.After(next) {
			next = sn.NextUpdateAt
		}
	}
	if attempted.IsZero() {
		return true
	}
	if now.Before(next) {
		return false
	}
	gap := info.MinInterval
	if force {
		gap = min(gap, forceFloor)
	}
	return now.Sub(attempted) >= gap
}

func (s *LiveService) store(ctx context.Context, info domain.LiveSourceInfo, snaps []domain.SourceSnapshot, fetchErr error, now time.Time) error {
	if fetchErr == nil {
		fetchErr = s.check(info, snaps)
	}
	if fetchErr != nil {
		msg := truncate(fetchErr.Error(), maxErrorLen)
		s.log.WarnContext(ctx, "fx live source failed", "source", info.ID, "error", msg)
		var errs []error
		for _, k := range info.Kinds {
			errs = append(errs, s.snaps.SaveFailure(ctx, info.ID, k, msg, now))
		}
		return errors.Join(errs...)
	}
	for _, sn := range snaps {
		sn.Source = info.ID
		if sn.FetchedAt.IsZero() {
			sn.FetchedAt = now
		}
		if err := s.snaps.SaveSuccess(ctx, sn); err != nil {
			return err
		}
	}
	s.log.InfoContext(ctx, "fx live source synced", "source", info.ID)
	return nil
}

// check rejects the whole payload of a source when any snapshot is invalid.
func (s *LiveService) check(info domain.LiveSourceInfo, snaps []domain.SourceSnapshot) error {
	seen := map[domain.SourceKind]bool{}
	for _, sn := range snaps {
		if !slices.Contains(info.Kinds, sn.Kind) || seen[sn.Kind] {
			return fmt.Errorf("unexpected snapshot kind %q", sn.Kind)
		}
		seen[sn.Kind] = true
		if err := sn.Validate(s.cfg.Local); err != nil {
			return err
		}
	}
	if len(seen) != len(info.Kinds) {
		return fmt.Errorf("payload is missing a kind (got %d of %d)", len(seen), len(info.Kinds))
	}
	return nil
}

// Board returns the composed board, cached in-process for boardCacheTTL.
func (s *LiveService) Board(ctx context.Context) (*LiveBoard, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, err
	}
	s.bootstrap(ctx)
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if s.cached != nil && s.now().Sub(s.cachedAt) < boardCacheTTL {
		return s.cached, nil
	}
	b, err := s.compose(ctx)
	if err != nil {
		return nil, err
	}
	s.cached, s.cachedAt = b, s.now()
	return b, nil
}

// bootstrap syncs once when no snapshot exists yet (fresh database, worker
// not yet ticked), so the first reader never sees an empty board.
func (s *LiveService) bootstrap(ctx context.Context) {
	if s.bootstrapped.Load() {
		return
	}
	stored, err := s.snaps.List(ctx)
	if err != nil {
		return
	}
	if len(stored) == 0 {
		if err := s.Sync(ctx, false); err != nil {
			s.log.WarnContext(ctx, "fx live bootstrap sync failed", "error", err)
			return
		}
	}
	s.bootstrapped.Store(true)
}

// Refresh syncs every source now (within forceFloor) and returns the new board.
func (s *LiveService) Refresh(ctx context.Context) (*LiveBoard, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, err
	}
	if err := s.Sync(ctx, true); err != nil {
		return nil, err
	}
	return s.Board(ctx)
}

func (s *LiveService) invalidate() {
	s.cacheMu.Lock()
	s.cached = nil
	s.cacheMu.Unlock()
}

func (s *LiveService) compose(ctx context.Context) (*LiveBoard, error) {
	stored, err := s.snaps.List(ctx)
	if err != nil {
		return nil, err
	}
	var ordered []domain.SourceSnapshot
	statuses := make([]SourceStatus, 0, len(s.sources))
	for _, src := range s.sources {
		info := src.Info()
		st := SourceStatus{Info: info, OK: true}
		found := 0
		for _, sn := range stored {
			if sn.Source != info.ID || !slices.Contains(info.Kinds, sn.Kind) {
				continue
			}
			ordered = append(ordered, sn)
			found++
			st.OK = st.OK && sn.OK
			if sn.Error != "" && st.Error == "" {
				st.Error = sn.Error
			}
			if !sn.FetchedAt.IsZero() && (st.FetchedAt.IsZero() || sn.FetchedAt.Before(st.FetchedAt)) {
				st.FetchedAt = sn.FetchedAt
			}
		}
		st.OK = st.OK && found == len(info.Kinds)
		statuses = append(statuses, st)
	}
	board := domain.ComposeBoard(ordered, s.now().UTC(), s.cfg)
	return &LiveBoard{Board: board, Sources: statuses, Disclaimer: s.disclaimer}, nil
}

// Adopt copies a live quote into fx_rates as base=currency, quote=local
// currency, audited as fx.rate_adopted in the same transaction.
func (s *LiveService) Adopt(ctx context.Context, in AdoptInput) (*domain.StoredRate, error) {
	if _, err := access.Require(ctx); err != nil {
		return nil, err
	}
	cur, err := domain.NormalizeCurrency(in.Currency)
	if err != nil {
		return nil, err
	}
	if cur == s.cfg.Local {
		return nil, shared.NewValidation("currency must differ from the local currency")
	}
	kind := domain.SourceKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	if kind != domain.KindOfficial && kind != domain.KindMarket {
		return nil, shared.NewValidation("kind must be official or market")
	}
	side := strings.ToLower(strings.TrimSpace(in.Side))
	if side == "" {
		side = "mid"
	}
	if side != "mid" && side != "buy" && side != "sell" {
		return nil, shared.NewValidation("side must be mid, buy or sell")
	}
	day := domain.DateOf(s.now().In(s.loc))
	if strings.TrimSpace(in.EffectiveDate) != "" {
		if day, err = domain.ParseDate(in.EffectiveDate); err != nil {
			return nil, err
		}
	}

	board, err := s.compose(ctx)
	if err != nil {
		return nil, err
	}
	q := pickSide(board.Board, cur, kind)
	if q == nil {
		return nil, &shared.AppError{
			Code: "live_quote_unavailable", Message: fmt.Sprintf("no live %s quote for %s", kind, cur),
			Err: shared.ErrInvalidState,
		}
	}
	scaled := q.Mid
	switch side {
	case "buy":
		scaled = q.Buy
	case "sell":
		scaled = q.Sell
	}
	rate := &domain.StoredRate{
		ID: uuid.New(),
		Rate: domain.Rate{
			Base: cur, Quote: s.cfg.Local, Scaled: scaled, EffectiveDate: day,
			Source: s.label(kind, q.Derived) + ":" + side,
		},
		CreatedAt: s.now().UTC(),
	}
	if in.ActorID != uuid.Nil {
		actor := in.ActorID
		rate.CreatedBy = &actor
	}
	extra := map[string]any{
		"kind": string(kind), "side": side, "derived": q.Derived, "stale": q.Stale,
		"observed_at": q.ObservedAt.UTC().Format(time.RFC3339),
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.rates.Insert(ctx, rate); err != nil {
			return mapInsertErr(err)
		}
		return recordRate(ctx, s.audit, in.ActorID, "fx.rate_adopted", rate.ID, nil, snapshot(rate), extra)
	})
	if err != nil {
		return nil, err
	}
	return rate, nil
}

// label names where a kind's rates come from ("lirascope:official"), or
// "derived:<kind>" for values computed via a reference cross.
func (s *LiveService) label(kind domain.SourceKind, derived bool) string {
	if !derived {
		for _, src := range s.sources {
			if info := src.Info(); slices.Contains(info.Kinds, kind) {
				return info.ID + ":" + string(kind)
			}
		}
	}
	return "derived:" + string(kind)
}

// AccountingProvider feeds fx.rates_sync with today's live mid of every
// pinned currency for kind (base=currency, quote=local currency).
func (s *LiveService) AccountingProvider(kind domain.SourceKind) domain.RateProvider {
	return accountingProvider{live: s, kind: kind}
}

type accountingProvider struct {
	live *LiveService
	kind domain.SourceKind
}

func (p accountingProvider) Name() string { return p.live.label(p.kind, false) }

// Fetch refuses a stale board and skips stale market quotes so accounting
// never books an outdated rate.
func (p accountingProvider) Fetch(ctx context.Context, on time.Time) ([]domain.Rate, error) {
	b, err := p.live.compose(ctx)
	if err != nil {
		return nil, err
	}
	if b.Stale {
		return nil, errors.New("live board is stale; accounting rates not written")
	}
	var out []domain.Rate
	for _, q := range b.Quotes {
		if !q.Pinned {
			continue
		}
		side := pickSide(b.Board, q.Currency, p.kind)
		if side == nil || side.Stale {
			continue
		}
		out = append(out, domain.Rate{
			Base: q.Currency, Quote: b.Local, Scaled: side.Mid, EffectiveDate: on,
			Source: p.live.label(p.kind, side.Derived),
		})
	}
	return out, nil
}

func pickSide(b domain.Board, cur string, kind domain.SourceKind) *domain.BoardSide {
	for _, q := range b.Quotes {
		if q.Currency != cur {
			continue
		}
		if kind == domain.KindOfficial {
			return q.Official
		}
		return q.Market
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}
