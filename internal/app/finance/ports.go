// Package finance orchestrates the finance hub. Each service depends only
// on the narrow ports it uses (ISP); adapters satisfy them (DIP).
package finance

import (
	"context"
	"time"

	"github.com/google/uuid"

	apppayment "github.com/wodi-crm/wodi-crm-be/internal/app/payment"
	appsupplier "github.com/wodi-crm/wodi-crm-be/internal/app/supplier"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	fxdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/fx"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	supplierdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
)

// Money is an amount in one currency.
type Money struct {
	Currency string `json:"currency"`
	Amount   int64  `json:"amount"`
	Count    int    `json:"count"`
}

// Settings are the branch finance parameters.
type Settings struct {
	ReportingCurrency string
	CommissionBPS     int
}

// SettingsStore reads and writes branch finance parameters. A nil branch
// (company-wide view) reads the defaults.
type SettingsStore interface {
	FinanceSettings(ctx context.Context, branchID *uuid.UUID) (Settings, error)
	SaveFinanceRates(ctx context.Context, branchID uuid.UUID, commissionBPS int) error
}

// PeriodPnL is one month in one currency: Revenue covers every booking,
// PnL only the costed ones (so uncosted sales never inflate the margin).
type PeriodPnL struct {
	Month    time.Time
	Currency string
	Revenue  int64
	PnL      domain.PnL
	Bookings int
}

// OverviewReader aggregates the executive dashboard figures.
type OverviewReader interface {
	CashByCurrency(ctx context.Context, branchID *uuid.UUID) ([]Money, error)
	ReceivablesByCurrency(ctx context.Context, branchID *uuid.UUID) ([]Money, error)
	PayablesByCurrency(ctx context.Context, branchID *uuid.UUID) ([]Money, error)
	DepositsByCurrency(ctx context.Context, branchID *uuid.UUID) ([]Money, error)
	MonthlyPnL(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]PeriodPnL, error)
	AlertCounts(ctx context.Context, branchID *uuid.UUID, today time.Time) (AlertCounts, error)
}

// AlertCounts are the things that need a finance person today.
type AlertCounts struct {
	LowDeposits       int
	LowAccounts       int
	UnmatchedCredits  int
	PendingRefunds    int
	SuspendedAgencies int
	OverdueSchedules  int
	SupplierDueSoon   int
}

// TreasuryStore persists accounts and their movement ledger.
type TreasuryStore interface {
	ListAccounts(ctx context.Context, branchID *uuid.UUID) ([]domain.Account, error)
	FindAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error)
	// LockAccount reads the account FOR UPDATE inside the caller's transaction.
	LockAccount(ctx context.Context, id uuid.UUID) (*domain.Account, error)
	InsertAccount(ctx context.Context, a *domain.Account) error
	UpdateAccount(ctx context.Context, a *domain.Account) error
	SetBalance(ctx context.Context, id uuid.UUID, balance int64) error
	InsertMovement(ctx context.Context, m *domain.Movement) (inserted bool, err error)
	FindMovement(ctx context.Context, id uuid.UUID) (*domain.Movement, error)
	ListMovements(ctx context.Context, f MovementFilter) ([]domain.Movement, error)
	ResolveMatch(ctx context.Context, id uuid.UUID, status string, bookingID, paymentID *uuid.UUID) error
	POSStats(ctx context.Context, branchID *uuid.UUID, since time.Time) ([]POSStat, error)
}

// MovementFilter narrows the movement list.
type MovementFilter struct {
	BranchID      *uuid.UUID
	AccountID     *uuid.UUID
	UnmatchedOnly bool
	Limit         int
}

// POSStat is one POS account's commission picture.
type POSStat struct {
	AccountID     uuid.UUID
	Name          string
	Currency      string
	CommissionBPS int
	Gross         int64
	Fees          int64
	Count         int
}

// BookingLookup finds open bookings for receivables work.
type BookingLookup interface {
	BookingsByRefs(ctx context.Context, branchID uuid.UUID, refNos []int64, pnrs []string) ([]domain.BookingDue, error)
	BookingDue(ctx context.Context, id uuid.UUID) (*BookingFacts, error)
}

// BookingFacts is what finance needs to know about one booking.
type BookingFacts struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	RefNo         int64
	PNR           string
	Status        string
	Currency      string
	Total         int64
	Collected     int64
	Balance       int64
	Cost          int64
	Tax           int64
	Fee           int64
	CustomerName  string
	CustomerPhone string
	CustomerEmail string
	AgencyID      *uuid.UUID
	ServiceType   string
}

// PaymentRecorder books customer money on a booking (the payment ledger).
type PaymentRecorder interface {
	Record(ctx context.Context, in apppayment.RecordInput) (*paymentdomain.Payment, error)
}

// AgencyStore persists B2B agencies and their booking links.
type AgencyStore interface {
	ListAgencies(ctx context.Context, branchID *uuid.UUID) ([]domain.Agency, error)
	FindAgency(ctx context.Context, id uuid.UUID) (*domain.Agency, error)
	LockAgency(ctx context.Context, id uuid.UUID) (*domain.Agency, error)
	InsertAgency(ctx context.Context, a *domain.Agency) error
	UpdateAgency(ctx context.Context, a *domain.Agency) error
	Exposures(ctx context.Context, branchID *uuid.UUID, today time.Time) (map[uuid.UUID]domain.Exposure, error)
	SetBookingAgency(ctx context.Context, bookingID uuid.UUID, agencyID *uuid.UUID) error
	ListActiveForSweep(ctx context.Context) ([]domain.Agency, error)
}

// ReceivablesReader reports what customers and agencies owe.
type ReceivablesReader interface {
	Ageing(ctx context.Context, branchID *uuid.UUID, today time.Time) ([]*domain.Ageing, error)
	TopDebtors(ctx context.Context, branchID *uuid.UUID, today time.Time, limit int) ([]Debtor, error)
}

// Debtor is a booking with an open balance.
type Debtor struct {
	BookingID    uuid.UUID
	RefNo        int64
	CustomerName string
	Phone        string
	AgencyID     *uuid.UUID
	AgencyName   string
	Currency     string
	Balance      int64
	DueOn        *time.Time
	DaysLate     int
}

// PayablesReader reports unpaid supplier invoices.
type PayablesReader interface {
	DueInvoices(ctx context.Context, branchID *uuid.UUID, until time.Time) ([]DueInvoice, error)
}

// SupplierLister lists supplier accounts with their funding figures.
type SupplierLister interface {
	List(ctx context.Context, f supplierdomain.ListFilter) ([]supplierdomain.Summary, error)
}

// DueInvoice is an unpaid supplier invoice on the payment plan.
type DueInvoice struct {
	ID            uuid.UUID
	SupplierID    uuid.UUID
	SupplierName  string
	InvoiceNumber string
	Status        string
	Currency      string
	Amount        int64
	DueOn         *time.Time
}

// SupplierAccounts is the supplier module seen from treasury: invoices
// and the supplier ledger.
type SupplierAccounts interface {
	Get(ctx context.Context, id uuid.UUID) (*supplierdomain.Supplier, error)
	GetInvoice(ctx context.Context, id uuid.UUID) (*supplierdomain.Invoice, error)
	UpdateInvoiceStatus(ctx context.Context, id uuid.UUID, to supplierdomain.InvoiceStatus) (*supplierdomain.Invoice, error)
	PostLedger(ctx context.Context, in appsupplier.LedgerInput) (*supplierdomain.LedgerEntry, *supplierdomain.Supplier, error)
}

// ProfitReader reports per-booking, per-departure and per-rep profit.
type ProfitReader interface {
	BookingPnL(ctx context.Context, f ProfitFilter) ([]BookingPnLRow, error)
	DeparturePnL(ctx context.Context, f ProfitFilter) ([]DeparturePnLRow, error)
	RepPnL(ctx context.Context, f ProfitFilter) ([]RepPnLRow, error)
}

// ProfitFilter is a booking-date window.
type ProfitFilter struct {
	BranchID *uuid.UUID
	From, To time.Time
	Limit    int
}

// BookingPnLRow is one booking's profit.
type BookingPnLRow struct {
	BookingID    uuid.UUID
	RefNo        int64
	CustomerName string
	OwnerName    string
	ServiceType  string
	Status       string
	Currency     string
	PnL          domain.PnL
	CreatedAt    time.Time
}

// DeparturePnLRow is one departure's actuals and budget.
type DeparturePnLRow struct {
	DepartureID uuid.UUID
	PackageName string
	DepartsOn   *time.Time
	Currency    string
	Bookings    int
	Pax         int
	PnL         domain.PnL
	Budget      *domain.Budget
}

// RepPnLRow is one sales rep's booking margins in one currency.
type RepPnLRow struct {
	UserID   uuid.UUID
	Name     string
	Currency string
	Margins  []int64
	PnL      domain.PnL
	Bookings int
	Uncosted int
}

// BudgetStore persists departure budgets.
type BudgetStore interface {
	DepartureBranch(ctx context.Context, departureID uuid.UUID) (uuid.UUID, error)
	UpsertBudget(ctx context.Context, branchID uuid.UUID, b domain.Budget, actor uuid.UUID) error
}

// BSPStore persists imported statements.
type BSPStore interface {
	SystemTickets(ctx context.Context, branchID uuid.UUID, from, to time.Time, currency string) ([]domain.SystemTicket, error)
	InsertStatement(ctx context.Context, st *domain.BSPStatement, lines []domain.BSPLine) error
	ListStatements(ctx context.Context, branchID *uuid.UUID, limit int) ([]domain.BSPStatement, error)
	FindStatement(ctx context.Context, id uuid.UUID) (*domain.BSPStatement, []domain.BSPLine, error)
}

// LetterStore persists balance confirmation letters.
type LetterStore interface {
	InsertLetter(ctx context.Context, l *domain.Letter) error
	ListLetters(ctx context.Context, branchID *uuid.UUID, limit int) ([]domain.Letter, error)
	FindLetterByToken(ctx context.Context, tokenHash string) (*domain.Letter, error)
	SaveLetterResponse(ctx context.Context, l *domain.Letter) error
	BranchName(ctx context.Context, branchID uuid.UUID) (string, error)
}

// Converter converts amounts for the reporting currency.
type Converter = fxdomain.Converter
