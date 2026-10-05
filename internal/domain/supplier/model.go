package supplier

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type LinkType string

const (
	LinkPackage   LinkType = "package"
	LinkDeparture LinkType = "departure"
	LinkService   LinkType = "service"
)

type ConfirmationStatus string

const (
	ConfirmPending   ConfirmationStatus = "pending"
	ConfirmConfirmed ConfirmationStatus = "confirmed"
	ConfirmCancelled ConfirmationStatus = "cancelled"
)

func ValidLinkType(t LinkType) bool {
	switch t {
	case LinkPackage, LinkDeparture, LinkService:
		return true
	default:
		return false
	}
}

func ValidConfirmStatus(s ConfirmationStatus) bool {
	switch s {
	case ConfirmPending, ConfirmConfirmed, ConfirmCancelled:
		return true
	default:
		return false
	}
}

// Supplier is a provider of inventory or services: its identity and account
// manager (Contact*), integration, financial account and service rules.
type Supplier struct {
	ID             uuid.UUID
	BranchID       uuid.UUID
	Code           string
	NameEn         string
	NameAr         string
	Category       string
	ContactName    string
	ContactPhone   string
	ContactEmail   string
	EmergencyPhone string
	// Terms holds the cancellation SLA and other service rules as text.
	Terms           string
	Integration     Integration
	Health          Health
	Finance         Finance
	Markups         Markups
	Regions         []string
	FreeCancelHours int
	ContractStart   *time.Time
	ContractEnd     *time.Time
	IsActive        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Link struct {
	ID                 uuid.UUID
	SupplierID         uuid.UUID
	LinkType           LinkType
	LinkID             uuid.UUID
	ConfirmationStatus ConfirmationStatus
	ConfirmationRef    string
	ConfirmedAt        *time.Time
	Allotment          int
	Sold               int
	UnitCost           int64
	Currency           string
	Notes              string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// IsOversold is true when allotment is capped and sold exceeds it.
func (l *Link) IsOversold() bool {
	return l.Allotment > 0 && l.Sold > l.Allotment
}

func (l *Link) Confirm(ref string) error {
	if l.ConfirmationStatus == ConfirmCancelled {
		return shared.NewInvalidState("cancelled link cannot be confirmed")
	}
	now := time.Now().UTC()
	l.ConfirmationStatus = ConfirmConfirmed
	l.ConfirmationRef = strings.TrimSpace(ref)
	l.ConfirmedAt = &now
	l.UpdatedAt = now
	return nil
}

func (l *Link) Cancel() error {
	now := time.Now().UTC()
	l.ConfirmationStatus = ConfirmCancelled
	l.UpdatedAt = now
	return nil
}

type Repository interface {
	Create(ctx context.Context, s *Supplier) error
	Update(ctx context.Context, s *Supplier) error
	FindByID(ctx context.Context, id uuid.UUID) (*Supplier, error)
	List(ctx context.Context, branchID *uuid.UUID, activeOnly bool) ([]Supplier, error)

	CreateLink(ctx context.Context, l *Link) error
	UpdateLink(ctx context.Context, l *Link) error
	FindLinkByID(ctx context.Context, id uuid.UUID) (*Link, error)
	DeleteLink(ctx context.Context, id uuid.UUID) error
	ListLinksBySupplier(ctx context.Context, supplierID uuid.UUID) ([]Link, error)
	ListUnconfirmed(ctx context.Context, branchID *uuid.UUID, limit int) ([]Link, error)
	ListOversold(ctx context.Context, branchID *uuid.UUID, limit int) ([]Link, error)

	CreateInvoice(ctx context.Context, inv *Invoice) error
	UpdateInvoice(ctx context.Context, inv *Invoice) error
	FindInvoiceByID(ctx context.Context, id uuid.UUID) (*Invoice, error)
	ListInvoices(ctx context.Context, branchID *uuid.UUID, supplierID *uuid.UUID, status *InvoiceStatus, limit int) ([]Invoice, error)
	ReplaceInvoiceLines(ctx context.Context, invoiceID uuid.UUID, lines []InvoiceLine) error
	ListInvoiceLines(ctx context.Context, invoiceID uuid.UUID) ([]InvoiceLine, error)

	CreateIssue(ctx context.Context, e *IssueEvent) error
	ListIssues(ctx context.Context, supplierID uuid.UUID, limit int) ([]IssueEvent, error)

	// FindForUpdate locks the supplier row for the rest of the transaction.
	FindForUpdate(ctx context.Context, id uuid.UUID) (*Supplier, error)
	ListSummaries(ctx context.Context, f ListFilter) ([]Summary, error)
	// LoadCredentials returns the sealed credential bag ("" when none).
	LoadCredentials(ctx context.Context, id uuid.UUID) (string, error)
	StoreCredentials(ctx context.Context, id uuid.UUID, sealed string) error
	ListContractsEnding(ctx context.Context, branchID *uuid.UUID, from, to time.Time) ([]Supplier, error)

	CreateLedgerEntry(ctx context.Context, e *LedgerEntry) error
	ListLedger(ctx context.Context, supplierID uuid.UUID, limit int) ([]LedgerEntry, error)
	VolumeSince(ctx context.Context, supplierID uuid.UUID, since time.Time) (Volume, error)

	AddUsage(ctx context.Context, u *Usage) error
	MetricsSince(ctx context.Context, supplierID uuid.UUID, since time.Time) (Metrics, error)

	CreateDispute(ctx context.Context, d *Dispute) error
	UpdateDispute(ctx context.Context, d *Dispute) error
	FindDispute(ctx context.Context, id uuid.UUID) (*Dispute, error)
	ListDisputes(ctx context.Context, supplierID uuid.UUID) ([]Dispute, error)
}

// ListFilter narrows the supplier directory.
type ListFilter struct {
	BranchID   *uuid.UUID
	Query      string
	Category   string
	ActiveOnly bool
	Since      time.Time
}

// Summary is a directory row: the supplier plus figures for list views.
type Summary struct {
	Supplier       Supplier
	HasCredentials bool
	OpenDisputes   int
	Spend          int64
	Bookings       int
}
