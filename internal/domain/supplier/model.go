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

type Supplier struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	Code         string
	NameEn       string
	NameAr       string
	ContactName  string
	ContactPhone string
	ContactEmail string
	Terms        string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (s *Supplier) Normalize() error {
	s.Code = strings.ToUpper(strings.TrimSpace(s.Code))
	s.NameEn = strings.TrimSpace(s.NameEn)
	s.NameAr = strings.TrimSpace(s.NameAr)
	s.ContactName = strings.TrimSpace(s.ContactName)
	s.ContactPhone = strings.TrimSpace(s.ContactPhone)
	s.ContactEmail = strings.TrimSpace(s.ContactEmail)
	s.Terms = strings.TrimSpace(s.Terms)
	if s.Code == "" {
		return shared.NewValidation("code is required")
	}
	if s.NameEn == "" && s.NameAr == "" {
		return shared.NewValidation("name_en or name_ar is required")
	}
	return nil
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
	List(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]Supplier, error)

	CreateLink(ctx context.Context, l *Link) error
	UpdateLink(ctx context.Context, l *Link) error
	FindLinkByID(ctx context.Context, id uuid.UUID) (*Link, error)
	DeleteLink(ctx context.Context, id uuid.UUID) error
	ListLinksBySupplier(ctx context.Context, supplierID uuid.UUID) ([]Link, error)
	ListUnconfirmed(ctx context.Context, branchID uuid.UUID, limit int) ([]Link, error)
	ListOversold(ctx context.Context, branchID uuid.UUID, limit int) ([]Link, error)

	CreateInvoice(ctx context.Context, inv *Invoice) error
	UpdateInvoice(ctx context.Context, inv *Invoice) error
	FindInvoiceByID(ctx context.Context, id uuid.UUID) (*Invoice, error)
	ListInvoices(ctx context.Context, branchID uuid.UUID, supplierID *uuid.UUID, status *InvoiceStatus, limit int) ([]Invoice, error)
	ReplaceInvoiceLines(ctx context.Context, invoiceID uuid.UUID, lines []InvoiceLine) error
	ListInvoiceLines(ctx context.Context, invoiceID uuid.UUID) ([]InvoiceLine, error)

	CreateIssue(ctx context.Context, e *IssueEvent) error
	ListIssues(ctx context.Context, supplierID uuid.UUID, limit int) ([]IssueEvent, error)
}
