package supplier

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Prober measures an integration endpoint (DIP; the adapter guards against
// SSRF).
type Prober interface {
	Probe(ctx context.Context, url string) (latency time.Duration, statusCode int, err error)
}

// FundsAlerts tells finance and operations about accounts and contracts that
// need action (DIP).
type FundsAlerts interface {
	SupplierLowBalance(ctx context.Context, sup *domain.Supplier) error
	SupplierContractExpiring(ctx context.Context, sup *domain.Supplier, daysLeft int) error
}

type Service struct {
	repo     domain.Repository
	tx       tx.Runner
	followUp ConfirmationFollowUp
	alerts   FundsAlerts
	audit    audit.Recorder
	secrets  crypto.SecretSealer
	prober   Prober
	now      func() time.Time
}

func NewService(repo domain.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, tx: txm, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) SetFollowUp(f ConfirmationFollowUp)    { s.followUp = f }
func (s *Service) SetFundsAlerts(a FundsAlerts)          { s.alerts = a }
func (s *Service) SetAuditor(a audit.Recorder)           { s.audit = a }
func (s *Service) SetSecrets(sealer crypto.SecretSealer) { s.secrets = sealer }
func (s *Service) SetProber(p Prober)                    { s.prober = p }

func (s *Service) today() time.Time { return domain.Day(s.now(), time.UTC) }

// ProfileInput is the full editable profile. Credentials are a patch: a
// non-empty value replaces, an empty value clears, a missing key is kept.
type ProfileInput struct {
	BranchID            uuid.UUID         `json:"-"`
	Code                string            `json:"code"`
	NameEn              string            `json:"name_en"`
	NameAr              string            `json:"name_ar"`
	Category            string            `json:"category"`
	ContactName         string            `json:"contact_name"`
	ContactPhone        string            `json:"contact_phone"`
	ContactEmail        string            `json:"contact_email"`
	EmergencyPhone      string            `json:"emergency_phone"`
	Terms               string            `json:"terms"`
	IntegrationType     string            `json:"integration_type"`
	Environment         string            `json:"environment"`
	APIBaseURL          string            `json:"api_base_url"`
	WebhookURL          string            `json:"webhook_url"`
	Credentials         map[string]string `json:"credentials"`
	PaymentModel        string            `json:"payment_model"`
	Currency            string            `json:"currency"`
	CreditLimit         int64             `json:"credit_limit"`
	LowBalanceThreshold int64             `json:"low_balance_threshold"`
	PaymentTerms        string            `json:"payment_terms"`
	Markups             map[string]int    `json:"markups"`
	Regions             []string          `json:"regions"`
	FreeCancelHours     int               `json:"free_cancel_hours"`
	ContractStart       string            `json:"contract_start"`
	ContractEnd         string            `json:"contract_end"`
	IsActive            *bool             `json:"is_active"`
}

func (in *ProfileInput) apply(sup *domain.Supplier) error {
	f := map[string]any{}
	start, err := optionalDay(in.ContractStart)
	if err != nil {
		f["contract_start"] = "YYYY-MM-DD"
	}
	end, err := optionalDay(in.ContractEnd)
	if err != nil {
		f["contract_end"] = "YYYY-MM-DD"
	}
	if len(f) > 0 {
		e := shared.NewValidation("invalid supplier")
		e.Details = f
		return e
	}
	sup.NameEn, sup.NameAr, sup.Category = in.NameEn, in.NameAr, in.Category
	sup.ContactName, sup.ContactPhone, sup.ContactEmail = in.ContactName, in.ContactPhone, in.ContactEmail
	sup.EmergencyPhone, sup.Terms = in.EmergencyPhone, in.Terms
	sup.Integration = domain.Integration{
		Type: in.IntegrationType, Environment: in.Environment, BaseURL: in.APIBaseURL, WebhookURL: in.WebhookURL,
	}
	sup.Finance.Model, sup.Finance.Currency = in.PaymentModel, in.Currency
	sup.Finance.CreditLimit, sup.Finance.LowBalanceThreshold = in.CreditLimit, in.LowBalanceThreshold
	sup.Finance.PaymentTerms = in.PaymentTerms
	sup.Markups = domain.Markups(in.Markups)
	sup.Regions = in.Regions
	sup.FreeCancelHours = in.FreeCancelHours
	sup.ContractStart, sup.ContractEnd = start, end
	if in.IsActive != nil {
		sup.IsActive = *in.IsActive
	}
	return nil
}

func optionalDay(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	d, err := domain.ParseDay(v)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Detail is everything the supplier workspace shows.
type Detail struct {
	Supplier        *domain.Supplier
	CredentialHints map[string]string
	Availability    domain.Availability
	ContractDays    *int
	Metrics         domain.Metrics
	Volume          domain.Volume
	Ledger          []domain.LedgerEntry
	Disputes        []domain.Dispute
	Today           time.Time
}

func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.Summary, error) {
	f.Since = s.today().AddDate(0, 0, -domain.MetricsWindowDays)
	return s.repo.ListSummaries(ctx, f)
}

// Today is the day availability is evaluated on.
func (s *Service) Today() time.Time { return s.today() }

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Supplier, error) {
	sup, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("supplier")
	}
	return sup, nil
}

// Detail loads the workspace; the ledger is only read when withLedger is set
// (finance permission).
func (s *Service) Detail(ctx context.Context, id uuid.UUID, withLedger bool) (*Detail, error) {
	sup, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	today := s.today()
	since := today.AddDate(0, 0, -domain.MetricsWindowDays)
	d := &Detail{Supplier: sup, Availability: sup.AvailabilityOn(today), Today: today, Ledger: []domain.LedgerEntry{}}
	if days, ok := sup.ContractDaysLeft(today); ok {
		d.ContractDays = &days
	}
	if d.CredentialHints, err = s.credentialHints(ctx, sup); err != nil {
		return nil, err
	}
	if d.Metrics, err = s.repo.MetricsSince(ctx, id, since); err != nil {
		return nil, err
	}
	if d.Volume, err = s.repo.VolumeSince(ctx, id, since); err != nil {
		return nil, err
	}
	if withLedger {
		if d.Ledger, err = s.repo.ListLedger(ctx, id, 50); err != nil {
			return nil, err
		}
	}
	if d.Disputes, err = s.repo.ListDisputes(ctx, id); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) Create(ctx context.Context, in ProfileInput) (*domain.Supplier, error) {
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	now := s.now()
	sup := &domain.Supplier{
		ID: uuid.New(), BranchID: in.BranchID, Code: in.Code, IsActive: true,
		Health: domain.Health{Status: domain.HealthUnknown}, CreatedAt: now, UpdatedAt: now,
	}
	if err := in.apply(sup); err != nil {
		return nil, err
	}
	if err := sup.Normalize(); err != nil {
		return nil, err
	}
	creds, err := domain.NormalizeCredentials(in.Credentials)
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, sup); err != nil {
			return err
		}
		changed, err := s.writeCredentials(ctx, sup, creds)
		if err != nil {
			return err
		}
		return s.record(ctx, "supplier.created", sup, nil, snapshot(sup), map[string]any{"credentials_changed": changed})
	})
	if err != nil {
		return nil, err
	}
	return sup, nil
}

// Update replaces the profile. The code is fixed after creation, and the
// payment model or currency cannot change while the account carries a balance.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in ProfileInput) (*domain.Supplier, error) {
	creds, err := domain.NormalizeCredentials(in.Credentials)
	if err != nil {
		return nil, err
	}
	var out *domain.Supplier
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		sup, err := s.repo.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		before := snapshot(sup)
		prev := sup.Finance
		if err := in.apply(sup); err != nil {
			return err
		}
		if err := sup.Normalize(); err != nil {
			return err
		}
		if prev.DepositBalance != 0 || prev.CreditUsed != 0 {
			f := map[string]any{}
			if sup.Finance.Model != prev.Model {
				f["payment_model"] = "settle the balance before changing the payment model"
			}
			if sup.Finance.Currency != prev.Currency {
				f["currency"] = "settle the balance before changing the currency"
			}
			if len(f) > 0 {
				e := shared.NewInvalidState("account has an open balance")
				e.Details = f
				return e
			}
		}
		sup.UpdatedAt = s.now()
		if err := s.repo.Update(ctx, sup); err != nil {
			return err
		}
		changed, err := s.writeCredentials(ctx, sup, creds)
		if err != nil {
			return err
		}
		out = sup
		return s.record(ctx, "supplier.updated", sup, before, snapshot(sup), map[string]any{"credentials_changed": changed})
	})
	return out, err
}

func (s *Service) SetActive(ctx context.Context, id uuid.UUID, active bool) (*domain.Supplier, error) {
	var out *domain.Supplier
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		sup, err := s.repo.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if sup.IsActive == active {
			out = sup
			return nil
		}
		sup.IsActive, sup.UpdatedAt = active, s.now()
		if err := s.repo.Update(ctx, sup); err != nil {
			return err
		}
		out = sup
		return s.record(ctx, "supplier.status_changed", sup, map[string]any{"is_active": !active}, map[string]any{"is_active": active}, nil)
	})
	return out, err
}

func credentialBinding(sup *domain.Supplier) crypto.Binding {
	return crypto.Binding{Table: "suppliers", RowID: sup.ID, BranchID: sup.BranchID}
}

func (s *Service) credentialHints(ctx context.Context, sup *domain.Supplier) (map[string]string, error) {
	if s.secrets == nil {
		return map[string]string{}, nil
	}
	sealed, err := s.repo.LoadCredentials(ctx, sup.ID)
	if err != nil {
		return nil, err
	}
	bag, err := s.secrets.Open(sealed, credentialBinding(sup))
	if err != nil {
		return nil, err
	}
	return shared.SecretHints(bag), nil
}

// writeCredentials merges the patch into the sealed bag and returns the
// names of the keys that changed (never their values).
func (s *Service) writeCredentials(ctx context.Context, sup *domain.Supplier, patch map[string]string) ([]string, error) {
	if len(patch) == 0 {
		return []string{}, nil
	}
	if s.secrets == nil {
		return nil, shared.NewInvalidState("credential storage is not configured")
	}
	sealed, err := s.repo.LoadCredentials(ctx, sup.ID)
	if err != nil {
		return nil, err
	}
	bag, err := s.secrets.Open(sealed, credentialBinding(sup))
	if err != nil {
		return nil, err
	}
	next := maps.Clone(bag)
	if next == nil {
		next = map[string]string{}
	}
	changed := []string{}
	for k, v := range patch {
		if v == "" {
			if _, ok := next[k]; ok {
				delete(next, k)
				changed = append(changed, k)
			}
			continue
		}
		if next[k] != v {
			next[k] = v
			changed = append(changed, k)
		}
	}
	if len(changed) == 0 {
		return changed, nil
	}
	slices.Sort(changed)
	out, err := s.secrets.Seal(next, credentialBinding(sup))
	if err != nil {
		return nil, err
	}
	return changed, s.repo.StoreCredentials(ctx, sup.ID, out)
}

// CredentialHints returns the masked credentials of a supplier.
func (s *Service) CredentialHints(ctx context.Context, id uuid.UUID) (map[string]string, error) {
	sup, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.credentialHints(ctx, sup)
}

// HealthCheck probes the API base URL and stores the result.
func (s *Service) HealthCheck(ctx context.Context, id uuid.UUID) (*domain.Supplier, error) {
	sup, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if sup.Integration.BaseURL == "" {
		e := shared.NewInvalidState("set an API base URL first")
		e.Details = map[string]any{"api_base_url": "required for a health check"}
		return nil, e
	}
	if s.prober == nil {
		return nil, shared.NewInvalidState("health checks are not configured")
	}
	latency, code, probeErr := s.prober.Probe(ctx, sup.Integration.BaseURL)
	status := domain.ClassifyProbe(latency, code, probeErr)
	note := ""
	if probeErr != nil {
		note = probeErr.Error()
	} else if code >= 400 {
		note = "HTTP " + itoa(code)
	}
	var out *domain.Supplier
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		sup, err := s.repo.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		at := s.now()
		sup.Health = domain.Health{Status: status, LatencyMs: int(latency.Milliseconds()), CheckedAt: &at, Note: trimNote(note)}
		sup.UpdatedAt = at
		out = sup
		return s.repo.Update(ctx, sup)
	})
	return out, err
}

// SetHealth records a manually reported status (e.g. announced maintenance).
func (s *Service) SetHealth(ctx context.Context, id uuid.UUID, status, note string) (*domain.Supplier, error) {
	var out *domain.Supplier
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		sup, err := s.repo.FindForUpdate(ctx, id)
		if err != nil {
			return err
		}
		before := map[string]any{"status": sup.Health.Status, "note": sup.Health.Note}
		at := s.now()
		if err := sup.Health.SetManual(status, note, at); err != nil {
			return err
		}
		sup.UpdatedAt = at
		if err := s.repo.Update(ctx, sup); err != nil {
			return err
		}
		out = sup
		return s.record(ctx, "supplier.health_set", sup, before, map[string]any{"status": sup.Health.Status, "note": sup.Health.Note}, nil)
	})
	return out, err
}

// RecordUsage adds a batch of search/booking counters from an integration.
func (s *Service) RecordUsage(ctx context.Context, u domain.Usage) (domain.Metrics, error) {
	if _, err := s.Get(ctx, u.SupplierID); err != nil {
		return domain.Metrics{}, err
	}
	today := s.today()
	if u.Day.IsZero() {
		u.Day = today
	}
	if err := u.Validate(today); err != nil {
		return domain.Metrics{}, err
	}
	if err := s.repo.AddUsage(ctx, &u); err != nil {
		return domain.Metrics{}, err
	}
	return s.repo.MetricsSince(ctx, u.SupplierID, today.AddDate(0, 0, -domain.MetricsWindowDays))
}

type LedgerInput struct {
	SupplierID uuid.UUID `json:"-"`
	ActorID    uuid.UUID `json:"-"`
	Kind       string    `json:"kind"`
	Amount     int64     `json:"amount"`
	Reference  string    `json:"reference"`
	Note       string    `json:"note"`
}

const (
	maxLedgerReference = 120
	maxLedgerNote      = 500
)

// PostLedger books a money movement under a row lock and alerts finance when
// the account falls below its threshold or is exhausted.
func (s *Service) PostLedger(ctx context.Context, in LedgerInput) (*domain.LedgerEntry, *domain.Supplier, error) {
	ref, note := trimTo(in.Reference, maxLedgerReference), trimTo(in.Note, maxLedgerNote)
	if in.Kind == domain.EntryAdjustment && note == "" {
		e := shared.NewValidation("adjustments need a reason")
		e.Details = map[string]any{"note": "required for adjustments"}
		return nil, nil, e
	}
	var entry *domain.LedgerEntry
	var out *domain.Supplier
	var crossed bool
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		sup, err := s.repo.FindForUpdate(ctx, in.SupplierID)
		if err != nil {
			return err
		}
		wasLevel := fundsLevel(sup.Finance)
		before := sup.Finance.Balance()
		balance, err := sup.Finance.Apply(in.Kind, in.Amount)
		if err != nil {
			return err
		}
		now := s.now()
		sup.UpdatedAt = now
		if err := s.repo.Update(ctx, sup); err != nil {
			return err
		}
		entry = &domain.LedgerEntry{
			ID: uuid.New(), SupplierID: sup.ID, BranchID: sup.BranchID, Kind: in.Kind, Amount: in.Amount,
			Currency: sup.Finance.Currency, BalanceAfter: balance, Reference: ref, Note: note, CreatedAt: now,
		}
		if in.ActorID != uuid.Nil {
			actor := in.ActorID
			entry.ActorID = &actor
		}
		if err := s.repo.CreateLedgerEntry(ctx, entry); err != nil {
			return err
		}
		crossed = fundsLevel(sup.Finance) > wasLevel
		out = sup
		return s.record(ctx, "supplier.ledger_posted", sup,
			map[string]any{"balance": before}, map[string]any{"balance": balance},
			map[string]any{"kind": in.Kind, "amount": in.Amount, "reference": ref})
	})
	if err != nil {
		return nil, nil, err
	}
	if crossed && s.alerts != nil {
		if err := s.alerts.SupplierLowBalance(ctx, out); err != nil {
			return entry, out, err
		}
	}
	return entry, out, nil
}

// fundsLevel grades an account: 0 healthy, 1 below threshold, 2 exhausted.
func fundsLevel(fi domain.Finance) int {
	switch {
	case fi.Exhausted():
		return 2
	case fi.LowBalance():
		return 1
	default:
		return 0
	}
}

func (s *Service) ListLedger(ctx context.Context, id uuid.UUID, limit int) ([]domain.LedgerEntry, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListLedger(ctx, id, limit)
}

// Routing ranks the branch's active suppliers for a product.
func (s *Service) Routing(ctx context.Context, branchID *uuid.UUID, product string) ([]domain.RouteOption, error) {
	if !slices.Contains(domain.Products, product) {
		e := shared.NewValidation("unknown product")
		e.Details = map[string]any{"product": "unknown product"}
		return nil, e
	}
	list, err := s.repo.List(ctx, branchID, false)
	if err != nil {
		return nil, err
	}
	return domain.Rank(list, product, s.today()), nil
}

type DisputeInput struct {
	SupplierID uuid.UUID `json:"-"`
	ActorID    uuid.UUID `json:"-"`
	Title      string    `json:"title"`
	BookingRef string    `json:"booking_ref"`
	Amount     int64     `json:"amount"`
	Currency   string    `json:"currency"`
}

func (s *Service) OpenDispute(ctx context.Context, in DisputeInput) (*domain.Dispute, error) {
	sup, err := s.Get(ctx, in.SupplierID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	d := &domain.Dispute{
		ID: uuid.New(), SupplierID: sup.ID, BranchID: sup.BranchID, Title: in.Title, BookingRef: in.BookingRef,
		Amount: in.Amount, Currency: in.Currency, Status: domain.DisputeOpen, OpenedAt: now, UpdatedAt: now,
	}
	if d.Currency == "" {
		d.Currency = sup.Finance.Currency
	}
	if in.ActorID != uuid.Nil {
		actor := in.ActorID
		d.OpenedBy = &actor
	}
	if err := d.Normalize(); err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateDispute(ctx, d); err != nil {
			return err
		}
		return s.record(ctx, "supplier.dispute_opened", sup, nil,
			map[string]any{"title": d.Title, "amount": d.Amount, "booking_ref": d.BookingRef}, nil)
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) CloseDispute(ctx context.Context, supplierID, disputeID uuid.UUID, status, resolution string) (*domain.Dispute, error) {
	sup, err := s.Get(ctx, supplierID)
	if err != nil {
		return nil, err
	}
	var out *domain.Dispute
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		d, err := s.repo.FindDispute(ctx, disputeID)
		if err != nil {
			return err
		}
		if d.SupplierID != supplierID {
			return shared.NewNotFound("dispute")
		}
		if err := d.Close(status, resolution, s.now()); err != nil {
			return err
		}
		if err := s.repo.UpdateDispute(ctx, d); err != nil {
			return err
		}
		out = d
		return s.record(ctx, "supplier.dispute_closed", sup, map[string]any{"status": domain.DisputeOpen},
			map[string]any{"status": d.Status, "resolution": d.Resolution}, map[string]any{"dispute_id": d.ID})
	})
	return out, err
}

// ContractExpirySweep alerts operations about contracts ending within the
// warning window (the alert itself is idempotent per day).
func (s *Service) ContractExpirySweep(ctx context.Context, branchID *uuid.UUID) (int, error) {
	if s.alerts == nil {
		return 0, nil
	}
	today := s.today()
	list, err := s.repo.ListContractsEnding(ctx, branchID, today, today.AddDate(0, 0, domain.ContractWarnDays))
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range list {
		days, _ := list[i].ContractDaysLeft(today)
		if err := s.alerts.SupplierContractExpiring(ctx, &list[i], days); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Service) record(ctx context.Context, action string, sup *domain.Supplier, before, after, extra map[string]any) error {
	if s.audit == nil {
		return nil
	}
	id, branch := sup.ID, sup.BranchID
	if extra == nil {
		extra = map[string]any{}
	}
	extra["code"] = sup.Code
	return s.audit.Record(ctx, audit.RecordInput{
		Action: action, EntityType: "supplier", EntityID: &id, BranchID: &branch,
		Before: before, After: after, Extra: extra,
	})
}

func snapshot(sup *domain.Supplier) map[string]any {
	return map[string]any{
		"name_en": sup.NameEn, "name_ar": sup.NameAr, "category": sup.Category, "contact_name": sup.ContactName,
		"contact_email": sup.ContactEmail, "integration_type": sup.Integration.Type,
		"environment": sup.Integration.Environment, "api_base_url": sup.Integration.BaseURL,
		"payment_model": sup.Finance.Model, "currency": sup.Finance.Currency, "credit_limit": sup.Finance.CreditLimit,
		"low_balance_threshold": sup.Finance.LowBalanceThreshold, "payment_terms": sup.Finance.PaymentTerms,
		"markups": sup.Markups, "regions": sup.Regions, "free_cancel_hours": sup.FreeCancelHours,
		"contract_end": optionalDayString(sup.ContractEnd), "is_active": sup.IsActive,
	}
}

func optionalDayString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return domain.FormatDay(*t)
}

func itoa(n int) string { return strconv.Itoa(n) }

func trimNote(v string) string { return trimTo(v, 300) }

func trimTo(v string, n int) string {
	v = strings.TrimSpace(v)
	if r := []rune(v); len(r) > n {
		return string(r[:n])
	}
	return v
}
