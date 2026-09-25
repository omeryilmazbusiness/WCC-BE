package supplier

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// InvoiceStatus for advanced supplier cost tracking (T-203).
type InvoiceStatus string

const (
	InvoiceDraft     InvoiceStatus = "draft"
	InvoiceSubmitted InvoiceStatus = "submitted"
	InvoiceApproved  InvoiceStatus = "approved"
	InvoicePaid      InvoiceStatus = "paid"
	InvoiceVoid      InvoiceStatus = "void"
)

func ValidInvoiceStatus(s InvoiceStatus) bool {
	switch s {
	case InvoiceDraft, InvoiceSubmitted, InvoiceApproved, InvoicePaid, InvoiceVoid:
		return true
	default:
		return false
	}
}

// Invoice aggregates supplier cost lines in minor currency units.
type Invoice struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	SupplierID    uuid.UUID
	InvoiceNumber string
	Status        InvoiceStatus
	Currency      string
	Subtotal      int64
	TaxTotal      int64
	GrandTotal    int64
	IssuedOn      *time.Time
	DueOn         *time.Time
	PaidAt        *time.Time
	Notes         string
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Lines         []InvoiceLine
}

// InvoiceLine is a cost line; may reference a supplier_link.
type InvoiceLine struct {
	ID          uuid.UUID
	InvoiceID   uuid.UUID
	LinkID      *uuid.UUID
	Description string
	Quantity    int
	UnitCost    int64
	LineTotal   int64
	SortOrder   int
	CreatedAt   time.Time
}

func (inv *Invoice) Normalize() error {
	inv.InvoiceNumber = strings.TrimSpace(inv.InvoiceNumber)
	inv.Currency = strings.ToUpper(strings.TrimSpace(inv.Currency))
	inv.Notes = strings.TrimSpace(inv.Notes)
	if inv.Currency == "" {
		inv.Currency = "SAR"
	}
	if !ValidInvoiceStatus(inv.Status) {
		inv.Status = InvoiceDraft
	}
	if inv.SupplierID == uuid.Nil {
		return shared.NewValidation("supplier_id is required")
	}
	return nil
}

// RecalcTotals recomputes money fields from lines (pure).
func (inv *Invoice) RecalcTotals() {
	var sub int64
	for i := range inv.Lines {
		q := inv.Lines[i].Quantity
		if q <= 0 {
			q = 1
			inv.Lines[i].Quantity = 1
		}
		inv.Lines[i].LineTotal = int64(q) * inv.Lines[i].UnitCost
		inv.Lines[i].SortOrder = i
		sub += inv.Lines[i].LineTotal
	}
	inv.Subtotal = sub
	if inv.TaxTotal < 0 {
		inv.TaxTotal = 0
	}
	inv.GrandTotal = inv.Subtotal + inv.TaxTotal
}

// CanTransition enforces simple invoice state machine (pure).
func CanTransition(from, to InvoiceStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case InvoiceDraft:
		return to == InvoiceSubmitted || to == InvoiceVoid
	case InvoiceSubmitted:
		return to == InvoiceApproved || to == InvoiceDraft || to == InvoiceVoid
	case InvoiceApproved:
		return to == InvoicePaid || to == InvoiceVoid
	case InvoicePaid, InvoiceVoid:
		return false
	default:
		return false
	}
}
