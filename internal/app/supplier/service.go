package supplier

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID     uuid.UUID `json:"-"`
	Code         string    `json:"code"`
	NameEn       string    `json:"name_en"`
	NameAr       string    `json:"name_ar"`
	ContactName  string    `json:"contact_name"`
	ContactPhone string    `json:"contact_phone"`
	ContactEmail string    `json:"contact_email"`
	Terms        string    `json:"terms"`
}

type UpdateInput struct {
	NameEn       *string `json:"name_en"`
	NameAr       *string `json:"name_ar"`
	ContactName  *string `json:"contact_name"`
	ContactPhone *string `json:"contact_phone"`
	ContactEmail *string `json:"contact_email"`
	Terms        *string `json:"terms"`
	IsActive     *bool   `json:"is_active"`
}

type LinkInput struct {
	SupplierID uuid.UUID      `json:"-"`
	LinkType   domain.LinkType `json:"link_type"`
	LinkID     uuid.UUID      `json:"link_id"`
	Allotment  int            `json:"allotment"`
	Sold       int            `json:"sold"`
	UnitCost   int64          `json:"unit_cost"`
	Currency   string         `json:"currency"`
	Notes      string         `json:"notes"`
}

type Service struct {
	repo  domain.Repository
	tx    tx.Runner
	queue shared.Enqueuer
}

func NewService(repo domain.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, tx: txm}
}

func (s *Service) SetEnqueuer(q shared.Enqueuer) { s.queue = q }

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Supplier, error) {
	now := time.Now().UTC()
	sup := &domain.Supplier{
		ID: uuid.New(), BranchID: in.BranchID, Code: in.Code,
		NameEn: in.NameEn, NameAr: in.NameAr, ContactName: in.ContactName,
		ContactPhone: in.ContactPhone, ContactEmail: in.ContactEmail,
		Terms: in.Terms, IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := sup.Normalize(); err != nil {
		return nil, err
	}
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	if err := s.repo.Create(ctx, sup); err != nil {
		return nil, err
	}
	return sup, nil
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (*domain.Supplier, error) {
	sup, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("supplier")
	}
	if in.NameEn != nil {
		sup.NameEn = *in.NameEn
	}
	if in.NameAr != nil {
		sup.NameAr = *in.NameAr
	}
	if in.ContactName != nil {
		sup.ContactName = *in.ContactName
	}
	if in.ContactPhone != nil {
		sup.ContactPhone = *in.ContactPhone
	}
	if in.ContactEmail != nil {
		sup.ContactEmail = *in.ContactEmail
	}
	if in.Terms != nil {
		sup.Terms = *in.Terms
	}
	if in.IsActive != nil {
		sup.IsActive = *in.IsActive
	}
	if err := sup.Normalize(); err != nil {
		return nil, err
	}
	sup.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, sup); err != nil {
		return nil, err
	}
	return sup, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Supplier, error) {
	sup, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("supplier")
	}
	return sup, nil
}

func (s *Service) List(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.Supplier, error) {
	return s.repo.List(ctx, branchID, activeOnly)
}

func (s *Service) Link(ctx context.Context, in LinkInput) (*domain.Link, error) {
	if !domain.ValidLinkType(in.LinkType) {
		return nil, shared.NewValidation("invalid link_type")
	}
	if in.SupplierID == uuid.Nil || in.LinkID == uuid.Nil {
		return nil, shared.NewValidation("supplier_id and link_id are required")
	}
	if _, err := s.repo.FindByID(ctx, in.SupplierID); err != nil {
		return nil, shared.NewNotFound("supplier")
	}
	cur := strings.ToUpper(strings.TrimSpace(in.Currency))
	if cur == "" {
		cur = "SAR"
	}
	now := time.Now().UTC()
	l := &domain.Link{
		ID: uuid.New(), SupplierID: in.SupplierID, LinkType: in.LinkType, LinkID: in.LinkID,
		ConfirmationStatus: domain.ConfirmPending, Allotment: in.Allotment, Sold: in.Sold,
		UnitCost: in.UnitCost, Currency: cur, Notes: strings.TrimSpace(in.Notes),
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateLink(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

func (s *Service) Unlink(ctx context.Context, linkID uuid.UUID) error {
	if err := s.repo.DeleteLink(ctx, linkID); err != nil {
		return shared.NewNotFound("supplier_link")
	}
	return nil
}

func (s *Service) Confirm(ctx context.Context, linkID uuid.UUID, ref string) (*domain.Link, error) {
	l, err := s.repo.FindLinkByID(ctx, linkID)
	if err != nil {
		return nil, shared.NewNotFound("supplier_link")
	}
	if err := l.Confirm(ref); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateLink(ctx, l); err != nil {
		return nil, err
	}
	return l, nil
}

func (s *Service) ListLinks(ctx context.Context, supplierID uuid.UUID) ([]domain.Link, error) {
	return s.repo.ListLinksBySupplier(ctx, supplierID)
}

func (s *Service) ListUnconfirmed(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.Link, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.repo.ListUnconfirmed(ctx, branchID, limit)
}

func (s *Service) ListOversold(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.Link, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.repo.ListOversold(ctx, branchID, limit)
}

func (s *Service) ProcessUnconfirmedReminders(ctx context.Context, branchID uuid.UUID, limit int) (int, error) {
	items, err := s.ListUnconfirmed(ctx, branchID, limit)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range items {
		if s.queue != nil {
			_, _ = s.queue.Enqueue(ctx, shared.JobReminderSend, []byte(`{"type":"supplier_unconfirmed","id":"`+items[i].ID.String()+`"}`), shared.EnqueueOpts{
				Queue: "low", UniqueKey: "supplier-unconfirmed:" + items[i].ID.String(),
			})
		}
		n++
	}
	return n, nil
}

// --- Epic 16 supplier invoices ---

type CreateInvoiceInput struct {
	BranchID      uuid.UUID `json:"-"`
	ActorID       uuid.UUID `json:"-"`
	SupplierID    uuid.UUID `json:"supplier_id"`
	InvoiceNumber string    `json:"invoice_number"`
	Currency      string    `json:"currency"`
	TaxTotal      int64     `json:"tax_total"`
	IssuedOn      *time.Time `json:"issued_on"`
	DueOn         *time.Time `json:"due_on"`
	Notes         string    `json:"notes"`
}

type SetInvoiceLinesInput struct {
	Description string     `json:"description"`
	LinkID      *uuid.UUID `json:"link_id"`
	Quantity    int        `json:"quantity"`
	UnitCost    int64      `json:"unit_cost"`
}

type UpdateInvoiceStatusInput struct {
	Status domain.InvoiceStatus `json:"status"`
}

func (s *Service) CreateInvoice(ctx context.Context, in CreateInvoiceInput) (*domain.Invoice, error) {
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	sup, err := s.repo.FindByID(ctx, in.SupplierID)
	if err != nil {
		return nil, shared.NewNotFound("supplier")
	}
	if sup.BranchID != in.BranchID {
		return nil, shared.NewForbidden("supplier branch mismatch")
	}
	now := time.Now().UTC()
	actor := in.ActorID
	inv := &domain.Invoice{
		ID: uuid.New(), BranchID: in.BranchID, SupplierID: in.SupplierID,
		InvoiceNumber: in.InvoiceNumber, Status: domain.InvoiceDraft,
		Currency: in.Currency, TaxTotal: in.TaxTotal,
		IssuedOn: in.IssuedOn, DueOn: in.DueOn, Notes: in.Notes,
		CreatedBy: &actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := inv.Normalize(); err != nil {
		return nil, err
	}
	inv.RecalcTotals()
	if err := s.repo.CreateInvoice(ctx, inv); err != nil {
		return nil, err
	}
	return inv, nil
}

func (s *Service) GetInvoice(ctx context.Context, branchID, id uuid.UUID) (*domain.Invoice, error) {
	inv, err := s.repo.FindInvoiceByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("supplier_invoice")
	}
	if inv.BranchID != branchID {
		return nil, shared.NewForbidden("invoice branch mismatch")
	}
	lines, err := s.repo.ListInvoiceLines(ctx, id)
	if err != nil {
		return nil, err
	}
	inv.Lines = lines
	return inv, nil
}

func (s *Service) ListInvoices(ctx context.Context, branchID uuid.UUID, supplierID *uuid.UUID, status *domain.InvoiceStatus, limit int) ([]domain.Invoice, error) {
	return s.repo.ListInvoices(ctx, branchID, supplierID, status, limit)
}

func (s *Service) UpdateInvoiceStatus(ctx context.Context, branchID, id uuid.UUID, to domain.InvoiceStatus) (*domain.Invoice, error) {
	inv, err := s.repo.FindInvoiceByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("supplier_invoice")
	}
	if inv.BranchID != branchID {
		return nil, shared.NewForbidden("invoice branch mismatch")
	}
	if !domain.ValidInvoiceStatus(to) {
		return nil, shared.NewValidation("invalid status")
	}
	if !domain.CanTransition(inv.Status, to) {
		return nil, shared.NewInvalidState("invalid invoice status transition")
	}
	now := time.Now().UTC()
	inv.Status = to
	if to == domain.InvoicePaid {
		inv.PaidAt = &now
	}
	inv.UpdatedAt = now
	if err := s.repo.UpdateInvoice(ctx, inv); err != nil {
		return nil, err
	}
	lines, _ := s.repo.ListInvoiceLines(ctx, id)
	inv.Lines = lines
	return inv, nil
}

func (s *Service) SetInvoiceLines(ctx context.Context, branchID, id uuid.UUID, inputs []SetInvoiceLinesInput) (*domain.Invoice, error) {
	inv, err := s.repo.FindInvoiceByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("supplier_invoice")
	}
	if inv.BranchID != branchID {
		return nil, shared.NewForbidden("invoice branch mismatch")
	}
	if inv.Status != domain.InvoiceDraft && inv.Status != domain.InvoiceSubmitted {
		return nil, shared.NewInvalidState("lines can only be edited in draft or submitted")
	}
	now := time.Now().UTC()
	lines := make([]domain.InvoiceLine, 0, len(inputs))
	for i, in := range inputs {
		q := in.Quantity
		if q <= 0 {
			q = 1
		}
		lines = append(lines, domain.InvoiceLine{
			ID: uuid.New(), InvoiceID: id, LinkID: in.LinkID,
			Description: strings.TrimSpace(in.Description),
			Quantity: q, UnitCost: in.UnitCost, SortOrder: i, CreatedAt: now,
		})
	}
	inv.Lines = lines
	inv.RecalcTotals()
	inv.UpdatedAt = now

	err = s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		if err := s.repo.ReplaceInvoiceLines(txCtx, id, inv.Lines); err != nil {
			return err
		}
		return s.repo.UpdateInvoice(txCtx, inv)
	})
	if err != nil {
		return nil, err
	}
	return inv, nil
}
