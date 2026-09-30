package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	aiprovider "github.com/wodi-crm/wodi-crm-be/internal/adapter/ai"
	extintadapter "github.com/wodi-crm/wodi-crm-be/internal/adapter/extint"
	filesyncprovider "github.com/wodi-crm/wodi-crm-be/internal/adapter/filesync"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/email"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/instagram"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/stub"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/integration/whatsapp"
	pgdash "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres"
	pgadminconfig "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/adminconfig"
	pgai "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/ai"
	pgaudit "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/audit"
	pgautomation "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/automation"
	pgbooking "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/booking"
	pgcustomer "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/customer"
	pgdocument "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/document"
	pgextint "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/extint"
	pgfilesync "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/filesync"
	pgidentity "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/identity"
	pgimportexport "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/importexport"
	pginbox "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/inbox"
	pglead "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/lead"
	pgnotification "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/notification"
	pgoutbox "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/outbox"
	pgpayment "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/payment"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgpii"
	pgrealtime "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/realtime"
	pgreport "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/report"
	pgrevenuetarget "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/revenuetarget"
	pgrooming "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/rooming"
	pgschedule "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/schedule"
	pgsearch "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/search"
	pgsupplier "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/supplier"
	pgtask "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/task"
	pkgpg "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/tourpackage"
	pgvisa "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/visa"
	pgwebhook "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/webhook"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/queue"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/sessioncache"
	"github.com/wodi-crm/wodi-crm-be/internal/adapter/storage"
	appadminconfig "github.com/wodi-crm/wodi-crm-be/internal/app/adminconfig"
	appai "github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	appauth "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/app/automation"
	appbooking "github.com/wodi-crm/wodi-crm-be/internal/app/booking"
	appcustomer "github.com/wodi-crm/wodi-crm-be/internal/app/customer"
	appdashboard "github.com/wodi-crm/wodi-crm-be/internal/app/dashboard"
	"github.com/wodi-crm/wodi-crm-be/internal/app/dataprotection"
	appdocument "github.com/wodi-crm/wodi-crm-be/internal/app/document"
	appextint "github.com/wodi-crm/wodi-crm-be/internal/app/extint"
	appfilesync "github.com/wodi-crm/wodi-crm-be/internal/app/filesync"
	appimportexport "github.com/wodi-crm/wodi-crm-be/internal/app/importexport"
	appinbox "github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	applead "github.com/wodi-crm/wodi-crm-be/internal/app/lead"
	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	appoutbox "github.com/wodi-crm/wodi-crm-be/internal/app/outbox"
	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	appprivacy "github.com/wodi-crm/wodi-crm-be/internal/app/privacy"
	appreport "github.com/wodi-crm/wodi-crm-be/internal/app/report"
	"github.com/wodi-crm/wodi-crm-be/internal/app/retention"
	apprevenuetarget "github.com/wodi-crm/wodi-crm-be/internal/app/revenuetarget"
	approoming "github.com/wodi-crm/wodi-crm-be/internal/app/rooming"
	appsearch "github.com/wodi-crm/wodi-crm-be/internal/app/search"
	appsupplier "github.com/wodi-crm/wodi-crm-be/internal/app/supplier"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	apppkg "github.com/wodi-crm/wodi-crm-be/internal/app/tourpackage"
	appuser "github.com/wodi-crm/wodi-crm-be/internal/app/useradmin"
	appvisa "github.com/wodi-crm/wodi-crm-be/internal/app/visa"
	appwebhook "github.com/wodi-crm/wodi-crm-be/internal/app/webhook"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	domaininbox "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/database"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/ratelimit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// modules is the composition shared by the API and the worker: both
// processes run the same services, reactors and outbox, so an event has the
// same effect wherever it is raised.
type modules struct {
	cfg     config.Config
	log     *slog.Logger
	pool    *pgxpool.Pool
	txm     *tx.Manager
	bus     *events.Bus
	queue   *queue.Client
	rdb     *redis.Client
	limiter ratelimit.Window
	keyring *crypto.Keyring

	outboxRepo *pgoutbox.Repository
	announcer  *pgrealtime.Notifier
	dispatcher *appoutbox.Dispatcher
	tokens     *platformauth.TokenService

	sessionCheck *sessioncache.Validator
	auth         *appauth.Service
	users        *appuser.Service
	audit        *appaudit.Service
	retention    *retention.Service
	customers    *appcustomer.Service
	leads        *applead.Service
	bookings     *appbooking.Service
	lifecycle    *appbooking.Lifecycle
	payments     *apppayment.Service
	finance      financeModule
	tasks        *apptask.Service
	targets      *apprevenuetarget.Service
	packages     *apppkg.Service
	dashboard    *appdashboard.Service
	documents    *appdocument.Service
	visas        *appvisa.Service
	suppliers    *appsupplier.Service
	notify       *appnotification.Service
	reports      *appreport.Service
	schedules    *appreport.Scheduler
	ai           *appai.Service
	fileSync     *appfilesync.Service
	extInt       *appextint.Service
	inbox        *appinbox.Service
	inboxAccts   *appinbox.WebhookAccounts
	adminConfig  *appadminconfig.Service
	rooming      *approoming.Service
	search       *appsearch.Service
	tenancy      tenancyModule
	imports      *appimportexport.Service
	backfill     *dataprotection.Service
	privacy      *appprivacy.Service
	webhooks     *appwebhook.Service
	jobs         *automation.Jobs
}

func buildModules(ctx context.Context, cfg config.Config, log *slog.Logger) (*modules, error) {
	pool, err := database.NewPool(ctx, cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	m := &modules{cfg: cfg, log: log, pool: pool}
	if err := m.build(); err != nil {
		m.close()
		return nil, err
	}
	return m, nil
}

func (m *modules) close() {
	if m.queue != nil {
		_ = m.queue.Close()
	}
	if m.rdb != nil {
		_ = m.rdb.Close()
	}
	if m.pool != nil {
		m.pool.Close()
	}
}

func (m *modules) build() error {
	cfg, log, pool := m.cfg, m.log, m.pool
	var err error
	m.queue, err = queue.NewClient(cfg.Redis, log, cfg.RequiresRedis())
	if err != nil {
		return fmt.Errorf("queue: %w", err)
	}
	m.keyring, err = NewKeyring(cfg.Auth)
	if err != nil {
		return fmt.Errorf("encryption keyring: %w", err)
	}
	m.limiter, m.rdb = newRateLimiter(cfg.Redis, log)
	m.txm = tx.NewManager(pool)
	m.bus = events.NewBus(log)
	m.outboxRepo = pgoutbox.NewRepository(pool)
	m.announcer = pgrealtime.NewNotifier(pool)
	outbox := appoutbox.NewWriter(m.outboxRepo)
	ledger := pgschedule.NewLedger(pool)
	txm, bus := m.txm, m.bus
	secretBox := crypto.NewSecretBox(m.keyring)
	passports := pgpii.NewPassports(m.keyring)
	store := storage.NewMinIO(cfg.Storage)
	loc := businessLocation(cfg)

	identityRepo := pgidentity.NewRepository(pool)
	customerRepo := pgcustomer.NewRepository(pool, passports)
	leadRepo := pglead.NewRepository(pool)
	bookingRepo := pgbooking.NewRepository(pool, passports)
	paymentRepo := pgpayment.NewRepository(pool)
	taskRepo := pgtask.NewRepository(pool)
	pkgRepo := pkgpg.NewRepository(pool)
	inboxRepo := pginbox.NewRepository(pool)
	directory := identityDirectory{repo: identityRepo}

	m.audit = appaudit.NewService(pgaudit.NewRepository(pool))
	securityRepo := pgidentity.NewSecurityRepository(identityRepo)
	m.sessionCheck = sessioncache.New(securityRepo, cfg.Auth.SessionCheckCacheTTL, sessioncache.DefaultMaxEntries)
	m.tokens, err = platformauth.NewTokenServiceFromConfig(cfg.Auth)
	if err != nil {
		return fmt.Errorf("access tokens: %w", err)
	}
	m.auth = newAuthService(cfg, identityRepo, securityRepo, m.audit, m.tokens, txm, m.keyring, m.limiter, m.sessionCheck)
	m.users = appuser.NewService(identityRepo, m.audit, txm, m.sessionCheck)
	m.retention = retention.NewService(securityRepo, pgwebhook.NewRepository(pool), m.audit, retention.DefaultPolicy)

	m.customers = appcustomer.NewService(customerRepo, txm)
	m.customers.SetAuditor(m.audit)
	m.leads = applead.NewService(leadRepo, txm, bus)
	m.leads.SetAuditor(m.audit)
	m.leads.SetOutbox(outbox)
	m.bookings = appbooking.NewService(bookingRepo, bookingRepo, pkgRepo, txm, bus)
	m.bookings.SetAuditor(m.audit)
	m.bookings.SetTimeZone(loc)
	m.leads.SetBookingCreator(leadBookingBridge{svc: m.bookings})
	m.leads.SetPackageChecker(leadPackageBridge{repo: pkgRepo})

	m.notify = appnotification.NewService(pgnotification.NewRepository(pool), txm, log)
	m.notify.SetUserDirectory(directory)
	m.notify.SetExternal(appnotification.LogExternal{Log: log})

	taskSeeder := apptask.NewSeeder(taskRepo, txm)
	alerts := automation.NewAlerts(m.notify, taskSeeder, directory, ledger, txm)

	m.payments = apppayment.NewService(paymentRepo, bookingRepo, txm, bus)
	m.payments.SetAuditor(m.audit)
	m.payments.SetOutbox(outbox)
	m.payments.SetTaskCreator(taskSeeder)
	m.payments.SetDueAlerts(alerts)
	m.tasks = apptask.NewService(taskRepo, txm, bus)
	m.tasks.SetConversationReader(taskConvBridge{repo: inboxRepo})
	m.tasks.SetOutbox(outbox)

	targetRepo := pgrevenuetarget.NewRepository(pool)
	m.targets = apprevenuetarget.NewService(targetRepo, txm)
	m.targets.SetAuditor(m.audit)
	m.targets.SetTaskCreator(taskSeeder)
	m.targets.SetOutbox(outbox)
	m.targets.SetBehindAlerts(alerts)
	m.packages = apppkg.NewService(pkgRepo, txm)
	m.packages.SetAuditor(m.audit)
	m.packages.SetBookingReader(bookingRepo)
	dashAgg := pgdash.NewDashboardAggregator(pool)
	m.dashboard = appdashboard.NewService(dashAgg)

	m.documents = appdocument.NewService(pgdocument.NewRepository(pool), store, txm)
	m.documents.SetAuditor(m.audit)
	m.documents.SetBookingContext(bookingDocsBridge{repo: bookingRepo})
	m.documents.SetOutbox(outbox)
	m.documents.SetExpiryAlerts(alerts)
	m.documents.SetRunLedger(ledger)
	m.bookings.SetDocReadiness(m.documents)
	m.lifecycle = appbooking.NewLifecycle(m.bookings, taskSeeder)

	m.visas = appvisa.NewService(pgvisa.NewRepository(pool), txm)
	m.visas.SetFollowUps(alerts)
	m.suppliers = appsupplier.NewService(pgsupplier.NewRepository(pool), txm)
	m.suppliers.SetFollowUp(alerts)

	m.finance = newFinanceModule(financeDeps{
		Cfg: cfg, Log: log, Pool: pool, Tx: txm, Auditor: m.audit, Payments: m.payments,
		Repo: paymentRepo, Bookings: bookingRepo, Tasks: taskSeeder, Notifier: m.notify,
	})
	m.dashboard.SetRevenueSources(dashAgg, paymentRepo, m.finance.FX.Converter())

	reportRepo := pgreport.NewRepository(pool)
	m.reports = appreport.NewService(reportRepo)
	m.schedules = appreport.NewScheduler(reportRepo, m.reports, reportRecipients{dir: directory}, alerts, txm, loc)

	aiRepo := pgai.NewRepository(pool)
	m.ai = appai.NewService(aiRepo, aiprovider.NewRegistry(
		aiprovider.NewOpenAI(),
		aiprovider.NewAnthropic(),
		aiprovider.NewGemini(),
	), secretBox)
	m.ai.SetAuditor(m.audit)
	m.ai.SetLeadPriorityWriter(aiRepo)
	m.ai.SetDashboardReader(aiDashBridge{dash: m.dashboard})
	m.ai.SetConversationReader(aiInboxBridge{repo: inboxRepo})
	m.ai.SetLeadReader(aiLeadBridge{repo: leadRepo, tasks: taskRepo})
	m.ai.SetTargetReader(aiTargetBridge{svc: m.targets})
	m.ai.SetLostLeadReader(aiLostBridge{repo: leadRepo})
	m.ai.SetDraftConversationReader(aiInboxBridge{repo: inboxRepo})
	m.ai.SetPackageCatalog(aiPackageCatalog{repo: pkgRepo})

	m.fileSync = appfilesync.NewService(pgfilesync.NewRepository(pool), filesyncprovider.NewRegistry(
		filesyncprovider.NewOneDrive(),
		filesyncprovider.NewSharePoint(),
	), secretBox)
	m.fileSync.SetOutbox(outbox)
	m.extInt = appextint.NewService(pgextint.NewRepository(pool), extintadapter.DefaultRegistry(), extintadapter.Catalog(), secretBox)
	m.extInt.SetOutbox(outbox)

	m.inbox = appinbox.NewService(inboxRepo, integration.NewRegistry(
		stub.New(),
		whatsapp.New(),
		instagram.New(),
		email.New(),
		stub.NewNamed(domaininbox.ChannelFacebook, "ok", ""),
		stub.NewNamed(domaininbox.ChannelGmail, "ok", ""),
	), txm, bus)
	m.inbox.SetSecrets(secretBox, m.keyring)
	m.inbox.SetCustomerMatcher(inboxCustomerBridge{repo: customerRepo})
	m.inbox.SetLeadShellCreator(inboxLeadBridge{svc: m.leads})
	m.inbox.SetLeadLocator(inboxLeadLocator{repo: leadRepo})
	m.inbox.SetOutbox(outbox)
	m.inboxAccts = appinbox.NewWebhookAccounts(inboxRepo, secretBox, m.keyring)

	m.adminConfig = appadminconfig.NewService(pgadminconfig.NewRepository(pool), txm)
	m.adminConfig.SetAuditor(m.audit)
	m.notify.SetRuleSource(m.adminConfig)
	m.tasks.SetGracePolicy(automation.TaskGrace{Rules: m.adminConfig})

	m.rooming = approoming.NewService(pgrooming.NewRepository(pool, passports))
	m.search = appsearch.NewService(pgsearch.NewRepository(pool, passports))
	m.tenancy = newTenancyModule(pool, m.users, m.ai, m.inbox, txm, m.audit)
	m.imports = appimportexport.NewService(pgimportexport.NewRepository(pool, passports), customerRepo, txm)
	m.imports.SetEnqueuer(m.queue)
	m.imports.SetQueueMode(m.queue)
	m.imports.SetOutbox(outbox)
	m.backfill = newEncryptBackfill(pool, m.keyring, m.audit)
	m.privacy = newPrivacyService(pool, customerRepo, bookingRepo, txm, m.audit, m.limiter)
	m.webhooks = newWebhookService(cfg, log, pool, m.inboxAccts, m.inbox, m.audit, outbox)

	m.lifecycle.Register(bus)
	apprevenuetarget.NewReactor(m.targets, bookingRepo).Register(bus)
	appnotification.NewReactor(m.notify).Register(bus)
	m.payments.RegisterReactors(bus)
	taskSeeder.Register(bus)
	apptask.NewReactor(taskRepo, bookingRepo, txm).Register(bus)
	automation.NewReactor(alerts, taskSeeder, bookingRecompute{l: m.lifecycle}, targetRecompute{svc: m.targets}, m.documents).Register(bus)

	m.dispatcher = appoutbox.NewDispatcher(m.outboxRepo, bus, m.announcer, appoutbox.Options{Log: log})
	m.jobs = automation.NewJobs(automation.JobDeps{
		Alerts: alerts, Tasks: taskSeeder, TaskRead: taskRepo, Queries: pgautomation.NewQueries(pool),
		Checklist: m.documents, SLA: m.inbox, Escalate: m.notify, Overdue: m.tasks,
		Payments: m.payments, Documents: m.documents, Suppliers: m.suppliers, Visas: m.visas,
		Targets: m.targets, Webhooks: m.webhooks, Reports: m.schedules, AI: m.ai, LostLeads: m.ai, AIReady: m.ai,
		Outbox: m.dispatcher, Log: log,
	})
	return nil
}

// bookingRecompute adapts the booking lifecycle to automation.BookingRecomputer.
type bookingRecompute struct {
	l *appbooking.Lifecycle
}

func (b bookingRecompute) RecomputeBooking(ctx context.Context, id uuid.UUID) error {
	_, err := b.l.Recompute(ctx, id)
	return err
}

// targetRecompute adapts revenue targets to automation.TargetRecomputer.
type targetRecompute struct {
	svc *apprevenuetarget.Service
}

func (t targetRecompute) RecomputeTargets(ctx context.Context, branchID uuid.UUID) error {
	_, err := t.svc.RecomputeBranch(ctx, branchID)
	return err
}

// reportRoles may receive scheduled report exports (reports.export holders).
var reportRoles = []string{string(platformauth.RoleGM), string(platformauth.RoleManager), string(platformauth.RoleAdmin)}

// reportRecipients adapts the user directory to report.Recipients.
type reportRecipients struct {
	dir identityDirectory
}

func (r reportRecipients) Eligible(ctx context.Context, branchID uuid.UUID) (map[uuid.UUID]bool, error) {
	users, err := r.dir.ListActiveByRoles(ctx, branchID, reportRoles)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]bool, len(users))
	for _, u := range users {
		out[u.ID] = true
	}
	return out, nil
}
