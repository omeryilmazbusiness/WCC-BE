package finance

import (
	"context"
	"errors"
	"sort"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// AgencyView is an agency with its live exposure and risk grade.
type AgencyView struct {
	Agency   domain.Agency
	Exposure domain.Exposure
	Risk     domain.Risk
}

// Receivables is the A/R screen.
type Receivables struct {
	Ageing   []*domain.Ageing
	Debtors  []Debtor
	Agencies []AgencyView
}

// ReceivablesService manages what customers and B2B agencies owe.
type ReceivablesService struct {
	Base
	agencies AgencyStore
	reader   ReceivablesReader
	bookings BookingLookup
	tx       tx.Runner
}

func NewReceivablesService(b Base, agencies AgencyStore, reader ReceivablesReader, bookings BookingLookup, txm tx.Runner) *ReceivablesService {
	return &ReceivablesService{Base: b, agencies: agencies, reader: reader, bookings: bookings, tx: txm}
}

// TopDebtorLimit bounds the debtor list.
const TopDebtorLimit = 25

func (s *ReceivablesService) Receivables(ctx context.Context, branchID *uuid.UUID) (*Receivables, error) {
	today := s.today()
	ageing, err := s.reader.Ageing(ctx, branchID, today)
	if err != nil {
		return nil, err
	}
	debtors, err := s.reader.TopDebtors(ctx, branchID, today, TopDebtorLimit)
	if err != nil {
		return nil, err
	}
	agencies, err := s.Agencies(ctx, branchID)
	if err != nil {
		return nil, err
	}
	return &Receivables{Ageing: ageing, Debtors: debtors, Agencies: agencies}, nil
}

// Agencies lists agencies riskiest first.
func (s *ReceivablesService) Agencies(ctx context.Context, branchID *uuid.UUID) ([]AgencyView, error) {
	list, err := s.agencies.ListAgencies(ctx, branchID)
	if err != nil {
		return nil, err
	}
	exp, err := s.agencies.Exposures(ctx, branchID, s.today())
	if err != nil {
		return nil, err
	}
	out := make([]AgencyView, 0, len(list))
	for _, a := range list {
		e := exp[a.ID]
		out = append(out, AgencyView{Agency: a, Exposure: e, Risk: a.RiskOf(e)})
	}
	rank := map[string]int{domain.RiskBlocked: 0, domain.RiskCritical: 1, domain.RiskWatch: 2, domain.RiskOK: 3}
	sort.SliceStable(out, func(i, j int) bool {
		if rank[out[i].Risk.Level] != rank[out[j].Risk.Level] {
			return rank[out[i].Risk.Level] < rank[out[j].Risk.Level]
		}
		return out[i].Exposure.Outstanding > out[j].Exposure.Outstanding
	})
	return out, nil
}

// AgencyInput is the editable agency profile.
type AgencyInput struct {
	Code             string `json:"code"`
	Name             string `json:"name"`
	ContactName      string `json:"contact_name"`
	Phone            string `json:"phone"`
	Email            string `json:"email"`
	TaxID            string `json:"tax_id"`
	Currency         string `json:"currency"`
	CreditLimit      int64  `json:"credit_limit"`
	PaymentTermsDays int    `json:"payment_terms_days"`
	GraceDays        int    `json:"grace_days"`
	AutoSuspend      *bool  `json:"auto_suspend"`
}

func (in AgencyInput) apply(a *domain.Agency) {
	a.Code, a.Name, a.ContactName, a.Phone, a.Email, a.TaxID = in.Code, in.Name, in.ContactName, in.Phone, in.Email, in.TaxID
	a.Currency, a.CreditLimit, a.PaymentTermsDays, a.GraceDays = in.Currency, in.CreditLimit, in.PaymentTermsDays, in.GraceDays
	if in.AutoSuspend != nil {
		a.AutoSuspend = *in.AutoSuspend
	}
}

func duplicateCode(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		e := shared.NewConflict("an agency with this code already exists")
		e.Details = map[string]any{"code": "already used"}
		return e
	}
	return err
}

func (s *ReceivablesService) CreateAgency(ctx context.Context, branchID, actor uuid.UUID, in AgencyInput) (*domain.Agency, error) {
	now := s.now()
	a := &domain.Agency{ID: uuid.New(), BranchID: branchID, Status: domain.AgencyActive, AutoSuspend: true, CreatedAt: now, UpdatedAt: now}
	in.apply(a)
	if err := a.Normalize(); err != nil {
		return nil, err
	}
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.agencies.InsertAgency(ctx, a); err != nil {
			return duplicateCode(err)
		}
		return s.record(ctx, actor, "agency.created", "agency", a.ID, branchID, a)
	})
	return a, err
}

func (s *ReceivablesService) UpdateAgency(ctx context.Context, id, actor uuid.UUID, in AgencyInput) (*domain.Agency, error) {
	var out *domain.Agency
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		a, err := s.agencies.LockAgency(ctx, id)
		if err != nil {
			return err
		}
		in.apply(a)
		if err := a.Normalize(); err != nil {
			return err
		}
		a.UpdatedAt = s.now()
		if err := s.agencies.UpdateAgency(ctx, a); err != nil {
			return duplicateCode(err)
		}
		out = a
		return s.record(ctx, actor, "agency.updated", "agency", a.ID, a.BranchID, a)
	})
	return out, err
}

// SetStatus suspends (manual), reactivates or closes an agency.
func (s *ReceivablesService) SetStatus(ctx context.Context, id, actor uuid.UUID, status string) (*domain.Agency, error) {
	var out *domain.Agency
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		a, err := s.agencies.LockAgency(ctx, id)
		if err != nil {
			return err
		}
		now := s.now()
		switch status {
		case domain.AgencySuspended:
			err = a.Suspend(domain.SuspendManual, now)
		case domain.AgencyActive:
			err = a.Reactivate(now)
		case domain.AgencyClosed:
			a.Status, a.SuspendReason, a.UpdatedAt = domain.AgencyClosed, "", now
		default:
			err = shared.NewValidation("status must be active, suspended or closed")
		}
		if err != nil {
			return err
		}
		if err := s.agencies.UpdateAgency(ctx, a); err != nil {
			return err
		}
		out = a
		return s.record(ctx, actor, "agency.status_changed", "agency", a.ID, a.BranchID, map[string]any{"status": a.Status})
	})
	return out, err
}

// AssignBooking puts a booking on an agency's open account after checking
// the agency may sell and its limit covers the booking's open balance.
func (s *ReceivablesService) AssignBooking(ctx context.Context, agencyID, bookingID, actor uuid.UUID) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		a, err := s.agencies.LockAgency(ctx, agencyID)
		if err != nil {
			return err
		}
		b, err := s.bookings.BookingDue(ctx, bookingID)
		if err != nil {
			return err
		}
		if b.BranchID != a.BranchID {
			return shared.NewValidation("booking belongs to another branch")
		}
		if b.AgencyID != nil && *b.AgencyID == a.ID {
			return nil
		}
		if b.Currency != a.Currency {
			return shared.NewValidation("booking currency differs from the agency account currency")
		}
		exp, err := s.agencies.Exposures(ctx, &a.BranchID, s.today())
		if err != nil {
			return err
		}
		if err := a.CanSell(exp[a.ID], b.Balance); err != nil {
			return err
		}
		if err := s.agencies.SetBookingAgency(ctx, bookingID, &a.ID); err != nil {
			return err
		}
		return s.record(ctx, actor, "agency.booking_assigned", "booking", bookingID, b.BranchID, map[string]any{"agency_id": a.ID})
	})
}

// UnassignBooking takes a booking off the agency account.
func (s *ReceivablesService) UnassignBooking(ctx context.Context, bookingID, actor uuid.UUID) error {
	b, err := s.bookings.BookingDue(ctx, bookingID)
	if err != nil {
		return err
	}
	if b.AgencyID == nil {
		return nil
	}
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.agencies.SetBookingAgency(ctx, bookingID, nil); err != nil {
			return err
		}
		return s.record(ctx, actor, "agency.booking_unassigned", "booking", bookingID, b.BranchID, map[string]any{"agency_id": *b.AgencyID})
	})
}

// SweepOverdue closes B2B sales for agencies whose debt is past the grace
// period (worker job, system scope). Returns how many were suspended.
func (s *ReceivablesService) SweepOverdue(ctx context.Context) (int, error) {
	ctx = access.WithScope(ctx, access.System())
	list, err := s.agencies.ListActiveForSweep(ctx)
	if err != nil || len(list) == 0 {
		return 0, err
	}
	exp, err := s.agencies.Exposures(ctx, nil, s.today())
	if err != nil {
		return 0, err
	}
	n := 0
	for _, cand := range list {
		if !cand.ShouldSuspend(exp[cand.ID]) {
			continue
		}
		err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
			a, err := s.agencies.LockAgency(ctx, cand.ID)
			if err != nil {
				return err
			}
			if !a.ShouldSuspend(exp[a.ID]) {
				return nil
			}
			if err := a.Suspend(domain.SuspendOverdue, s.now()); err != nil {
				return err
			}
			if err := s.agencies.UpdateAgency(ctx, a); err != nil {
				return err
			}
			n++
			return s.record(ctx, uuid.Nil, "agency.auto_suspended", "agency", a.ID, a.BranchID, map[string]any{
				"overdue": exp[a.ID].Overdue, "oldest_overdue_days": exp[a.ID].OldestOverdueDays,
			})
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
