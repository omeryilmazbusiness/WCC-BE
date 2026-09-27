package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/fxlive"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/fxprovider"
	fxhttp "github.com/wodi-crm/wodi-crm-be/internal/adapter/http/fx"
	pgaudit "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/audit"
	pgbooking "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/booking"
	pgfx "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/fx"
	pgnotification "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/notification"
	pgpayment "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	pgtask "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/task"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	appfx "github.com/wodi-crm/wodi-crm-be/internal/app/fx"
	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	notificationdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// overdueScheduleBatch bounds one finance.schedules_overdue transaction.
const overdueScheduleBatch = 500

// financeModule holds the Epic 21 finance collaborators shared by the API
// and the worker.
type financeModule struct {
	FX       *appfx.Service
	Promises *apppayment.Promises
	// Live is nil when FX_LIVE_ENABLED=false.
	Live *appfx.LiveService
}

type financeDeps struct {
	Cfg      config.Config
	Log      *slog.Logger
	Pool     *pgxpool.Pool
	Tx       tx.Runner
	Auditor  audit.Recorder
	Payments *apppayment.Service
	Repo     *pgpayment.Repository
	Bookings *pgbooking.Repository
	Tasks    *apptask.Seeder
	Notifier *appnotification.Service
}

// newFinanceModule builds FX + promises and attaches them to the payment service.
func newFinanceModule(d financeDeps) financeModule {
	loc := businessLocation(d.Cfg)
	fxRepo := pgfx.NewRepository(d.Pool)
	var live *appfx.LiveService
	if d.Cfg.FX.LiveEnabled {
		live = newLiveService(d, fxRepo, loc)
	}
	var providers []fxdomain.RateProvider
	if d.Cfg.FX.ProviderURL != "" {
		providers = append(providers, fxprovider.NewHTTP(d.Cfg.FX.ProviderURL, d.Cfg.FX.ProviderBase, d.Cfg.FX.ProviderTimeout))
	}
	if live != nil && d.Cfg.FX.AccountingEnabled() {
		providers = append(providers, live.AccountingProvider(fxdomain.SourceKind(d.Cfg.FX.AccountingSource)))
	}
	fxSvc := appfx.NewService(fxRepo, d.Tx, d.Auditor, appfx.Options{
		Providers: providers, Location: loc, Log: d.Log,
	})
	clock := apppayment.Clock{Location: loc}
	promises := apppayment.NewPromises(d.Repo, d.Bookings, d.Tx, d.Auditor, apppayment.PromiseDeps{
		Tasks: d.Tasks, Notifier: promiseNotifier{svc: d.Notifier}, Clock: clock, Log: d.Log,
	})
	d.Payments.SetFinance(apppayment.FinanceDeps{
		Converter: fxSvc.Converter(), Bookings: d.Repo, Promises: promises, Clock: clock, Log: d.Log,
	})
	return financeModule{FX: fxSvc, Promises: promises, Live: live}
}

const liveDisclaimer = "Indicative rates in new Syrian pounds (1 new SYP = 100 old SYP since 2026-01-01) from " +
	"third-party sources: the Central Bank of Syria official rate and the Damascus parallel market via LiraScope, " +
	"and ExchangeRate-API reference crosses. Derived values combine a USD rate with a reference cross. " +
	"Accounting uses only rates stored in fx_rates."

// newLiveService wires the Damascus live board: LiraScope (official + market)
// and ExchangeRate-API (reference crosses).
func newLiveService(d financeDeps, rates fxdomain.Repository, loc *time.Location) *appfx.LiveService {
	fx := d.Cfg.FX
	sources := []fxdomain.LiveSource{
		fxlive.NewLiraScope(fxlive.LiraScopeConfig{
			BaseURL: fx.LiraScopeBaseURL, APIKey: fx.LiraScopeAPIKey, APISecret: fx.LiraScopeAPISecret, Timeout: fx.LiveTimeout,
		}),
		fxlive.NewERAPI(fx.ERAPIBaseURL, fx.LiveTimeout),
	}
	return appfx.NewLiveService(sources, pgfx.NewLiveRepository(d.Pool), rates, d.Tx, d.Auditor, appfx.LiveOptions{
		Board: fxdomain.BoardConfig{
			Local: fx.LocalCurrency, Pinned: fx.LivePinned, Currencies: fx.LiveCurrencies, MarketMaxAge: fx.LiveMarketMaxAge,
		},
		Disclaimer: liveDisclaimer, Location: loc, Log: d.Log,
	})
}

// liveHandler keeps Svc a nil interface when the board is disabled.
func liveHandler(live *appfx.LiveService) fxhttp.LiveHandler {
	if live == nil {
		return fxhttp.LiveHandler{}
	}
	return fxhttp.LiveHandler{Svc: live}
}

// businessLocation is the zone calendar dates (received_at, promised_on,
// rate effective dates) are taken in; Validate guarantees it loads.
func businessLocation(cfg config.Config) *time.Location {
	loc, err := time.LoadLocation(cfg.Redis.SchedulerTZ)
	if err != nil {
		return time.UTC
	}
	return loc
}

// promiseNotifier adapts notification.Service to payment.PromiseNotifier (DIP).
type promiseNotifier struct {
	svc *appnotification.Service
}

func (n promiseNotifier) NotifyPromiseBroken(ctx context.Context, p *paymentdomain.Promise, ownerID uuid.UUID) error {
	if n.svc == nil {
		return nil
	}
	bookingID := p.BookingID
	_, err := n.svc.Emit(ctx, appnotification.EmitInput{
		BranchID: p.BranchID, RecipientUserID: ownerID, Kind: notificationdomain.KindPaymentOverdue,
		Title:      "Payment promise broken",
		Body:       fmt.Sprintf("%d %s promised for %s was not collected", p.Amount, p.Currency, p.PromisedOn.Format(time.DateOnly)),
		EntityType: "booking", EntityID: &bookingID, HrefHint: "/bookings/" + bookingID.String(),
	})
	return err
}

// FinanceJobs are the Epic 21 finance worker handlers.
type FinanceJobs struct {
	fx       *appfx.Service
	live     *appfx.LiveService
	payments *apppayment.Service
	promises *apppayment.Promises
	log      *slog.Logger
}

func NewFinanceJobs(pool *pgxpool.Pool, passports *pgpii.Passports, cfg config.Config, log *slog.Logger) *FinanceJobs {
	txm := tx.NewManager(pool)
	auditor := appaudit.NewService(pgaudit.NewRepository(pool))
	paymentRepo := pgpayment.NewRepository(pool)
	bookingRepo := pgbooking.NewRepository(pool, passports)
	payments := apppayment.NewService(paymentRepo, bookingRepo, txm, events.NewBus(log))
	payments.SetAuditor(auditor)
	notifier := appnotification.NewService(pgnotification.NewRepository(pool), txm, log)
	notifier.SetExternal(appnotification.LogExternal{Log: log})
	fin := newFinanceModule(financeDeps{
		Cfg: cfg, Log: log, Pool: pool, Tx: txm, Auditor: auditor, Payments: payments,
		Repo: paymentRepo, Bookings: bookingRepo, Tasks: apptask.NewSeeder(pgtask.NewRepository(pool), txm),
		Notifier: notifier,
	})
	return &FinanceJobs{fx: fin.FX, live: fin.Live, payments: payments, promises: fin.Promises, log: log}
}

// SyncLive handles fx.live_sync (no-op when the live board is disabled).
func (j *FinanceJobs) SyncLive(ctx context.Context, _ []byte) error {
	if j.live == nil {
		return nil
	}
	return j.live.Sync(ctx, false)
}

// SyncRates handles fx.rates_sync (no-op without FX_PROVIDER_URL and FX_ACCOUNTING_SOURCE).
func (j *FinanceJobs) SyncRates(ctx context.Context, _ []byte) error {
	n, err := j.fx.SyncRates(ctx)
	if err == nil && n > 0 {
		j.log.InfoContext(ctx, "fx rates synced", "inserted", n)
	}
	return err
}

// CheckPromises handles finance.promises_check.
func (j *FinanceJobs) CheckPromises(ctx context.Context, _ []byte) error {
	kept, broken, err := j.promises.ResolveDue(ctx)
	if kept+broken > 0 {
		j.log.InfoContext(ctx, "payment promises resolved", "kept", kept, "broken", broken)
	}
	return err
}

// MarkSchedulesOverdue handles finance.schedules_overdue.
func (j *FinanceJobs) MarkSchedulesOverdue(ctx context.Context, _ []byte) error {
	n, err := j.payments.MarkOverdueSchedules(ctx, overdueScheduleBatch)
	if n > 0 {
		j.log.InfoContext(ctx, "payment schedules marked overdue", "count", n)
	}
	return err
}
