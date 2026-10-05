package finance

import (
	"context"
	"time"

	"github.com/google/uuid"

	appsupplier "github.com/wodi-crm/wodi-crm-be/internal/app/supplier"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	supplierdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// PlanHorizonDays is how far ahead the supplier payment plan looks.
const PlanHorizonDays = 60

// Payables is the A/P screen.
type Payables struct {
	Suppliers []supplierdomain.Summary
	Plan      []DueInvoice
	Today     time.Time
}

// PayablesService manages what the agency owes suppliers and pays it out
// of treasury.
type PayablesService struct {
	Base
	reader    PayablesReader
	lister    SupplierLister
	suppliers SupplierAccounts
	treasury  TreasuryStore
	tx        tx.Runner
	ledger    *TreasuryService
}

func NewPayablesService(b Base, reader PayablesReader, lister SupplierLister, suppliers SupplierAccounts, ledger *TreasuryService, store TreasuryStore, txm tx.Runner) *PayablesService {
	return &PayablesService{Base: b, reader: reader, lister: lister, suppliers: suppliers, ledger: ledger, treasury: store, tx: txm}
}

func (s *PayablesService) Payables(ctx context.Context, branchID *uuid.UUID) (*Payables, error) {
	sups, err := s.lister.List(ctx, supplierdomain.ListFilter{BranchID: branchID, ActiveOnly: true})
	if err != nil {
		return nil, err
	}
	plan, err := s.reader.DueInvoices(ctx, branchID, s.today().AddDate(0, 0, PlanHorizonDays))
	if err != nil {
		return nil, err
	}
	return &Payables{Suppliers: sups, Plan: plan, Today: s.today()}, nil
}

// PayInvoiceInput pays an approved supplier invoice from an account.
type PayInvoiceInput struct {
	InvoiceID uuid.UUID `json:"-"`
	AccountID uuid.UUID `json:"account_id"`
	Fee       int64     `json:"fee"`
	Note      string    `json:"note"`
}

// PayInvoice books the treasury outflow, marks the invoice paid and, for a
// credit-line supplier, settles the outstanding amount on its ledger — all
// in one transaction.
func (s *PayablesService) PayInvoice(ctx context.Context, actor uuid.UUID, in PayInvoiceInput) (*domain.Movement, error) {
	var out *domain.Movement
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		inv, err := s.suppliers.GetInvoice(ctx, in.InvoiceID)
		if err != nil {
			return err
		}
		if inv.Status != supplierdomain.InvoiceApproved {
			return shared.NewInvalidState("only approved invoices can be paid")
		}
		if inv.GrandTotal <= 0 {
			return shared.NewInvalidState("invoice total is zero")
		}
		sup, err := s.suppliers.Get(ctx, inv.SupplierID)
		if err != nil {
			return err
		}
		acc, err := s.treasury.LockAccount(ctx, in.AccountID)
		if err != nil {
			return err
		}
		if acc.Currency != inv.Currency {
			return shared.NewValidation("pay from an account in the invoice currency")
		}
		if acc.BranchID != inv.BranchID {
			return shared.NewValidation("account belongs to another branch")
		}
		supplierID := sup.ID
		m := &domain.Movement{
			ID: uuid.New(), Direction: domain.DirectionOut, Kind: domain.MoveSupplierPayment, Amount: inv.GrandTotal, Fee: in.Fee,
			SupplierID: &supplierID, Reference: inv.InvoiceNumber, Counterparty: sup.NameEn, Note: in.Note,
			OccurredOn: s.today(), ActorID: &actor, CreatedAt: s.now(),
		}
		if err := s.ledger.post(ctx, acc, m); err != nil {
			return err
		}
		if _, err := s.suppliers.UpdateInvoiceStatus(ctx, inv.ID, supplierdomain.InvoicePaid); err != nil {
			return err
		}
		if sup.Finance.Model == supplierdomain.PaymentPostpaid && sup.Finance.Currency == inv.Currency && sup.Finance.CreditUsed > 0 {
			settle := min(inv.GrandTotal, sup.Finance.CreditUsed)
			if _, _, err := s.suppliers.PostLedger(ctx, appsupplier.LedgerInput{
				SupplierID: sup.ID, ActorID: actor, Kind: supplierdomain.EntryPayment, Amount: settle,
				Reference: inv.InvoiceNumber, Note: "Paid from " + acc.Name,
			}); err != nil {
				return err
			}
		}
		out = m
		return s.record(ctx, actor, "payables.invoice_paid", "supplier_invoice", inv.ID, inv.BranchID, map[string]any{
			"movement_id": m.ID, "account_id": acc.ID, "amount": inv.GrandTotal, "currency": inv.Currency,
		})
	})
	return out, err
}

// TopUpInput funds a prepaid supplier deposit from an account.
type TopUpInput struct {
	SupplierID uuid.UUID `json:"-"`
	AccountID  uuid.UUID `json:"account_id"`
	Amount     int64     `json:"amount"`
	Fee        int64     `json:"fee"`
	Reference  string    `json:"reference"`
}

// TopUpDeposit wires money to a prepaid supplier (Duffel, RateHawk, a DMC)
// and credits the deposit on its ledger in one transaction.
func (s *PayablesService) TopUpDeposit(ctx context.Context, actor uuid.UUID, in TopUpInput) (*domain.Movement, error) {
	var out *domain.Movement
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		sup, err := s.suppliers.Get(ctx, in.SupplierID)
		if err != nil {
			return err
		}
		if sup.Finance.Model != supplierdomain.PaymentPrepaid {
			return shared.NewInvalidState("top-ups apply to prepaid suppliers")
		}
		acc, err := s.treasury.LockAccount(ctx, in.AccountID)
		if err != nil {
			return err
		}
		if acc.Currency != sup.Finance.Currency {
			return shared.NewValidation("pay from an account in the supplier currency")
		}
		if acc.BranchID != sup.BranchID {
			return shared.NewValidation("account belongs to another branch")
		}
		supplierID := sup.ID
		m := &domain.Movement{
			ID: uuid.New(), Direction: domain.DirectionOut, Kind: domain.MoveSupplierPayment, Amount: in.Amount, Fee: in.Fee,
			SupplierID: &supplierID, Reference: in.Reference, Counterparty: sup.NameEn, Note: "Deposit top-up",
			OccurredOn: s.today(), ActorID: &actor, CreatedAt: s.now(),
		}
		if err := s.ledger.post(ctx, acc, m); err != nil {
			return err
		}
		if _, _, err := s.suppliers.PostLedger(ctx, appsupplier.LedgerInput{
			SupplierID: sup.ID, ActorID: actor, Kind: supplierdomain.EntryTopUp, Amount: in.Amount,
			Reference: in.Reference, Note: "Wired from " + acc.Name,
		}); err != nil {
			return err
		}
		out = m
		return s.record(ctx, actor, "payables.deposit_topped_up", "supplier", sup.ID, sup.BranchID, map[string]any{
			"movement_id": m.ID, "account_id": acc.ID, "amount": in.Amount, "currency": acc.Currency,
		})
	})
	return out, err
}
