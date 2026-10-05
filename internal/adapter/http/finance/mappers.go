package finance

import (
	"time"

	"github.com/google/uuid"

	app "github.com/wodi-crm/wodi-crm-be/internal/app/finance"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
	supplierdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func day(t time.Time) string { return t.Format(time.DateOnly) }

func optDay(t *time.Time) any {
	if t == nil {
		return nil
	}
	return day(*t)
}

func optTS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func optID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

func mapPnL(p domain.PnL) map[string]any {
	return map[string]any{
		"gross": p.Gross, "net": p.Net, "tax": p.Tax, "fee": p.Fee, "costed": p.Costed,
		"revenue": p.Revenue(), "margin": p.Margin(), "margin_bps": p.MarginBPS(),
	}
}

func mapMonth(m app.MonthFigure) map[string]any {
	return map[string]any{
		"month": m.Month.Format("2006-01"), "revenue": m.Revenue, "margin": m.Margin,
		"bookings": m.Bookings, "partial": m.Partial,
	}
}

func mapOverview(o *app.Overview) map[string]any {
	trend := make([]map[string]any, 0, len(o.Trend))
	for _, m := range o.Trend {
		trend = append(trend, mapMonth(m))
	}
	exposure := make([]map[string]any, 0, len(o.Exposure))
	for _, s := range o.Exposure {
		exposure = append(exposure, map[string]any{
			"currency": s.Currency, "amount": s.Amount, "converted": s.Converted,
			"share_bps": s.ShareBPS, "unconverted": s.Unconverted,
		})
	}
	a := o.Alerts
	return map[string]any{
		"reporting_currency": o.ReportingCurrency,
		"cash":               o.Cash, "receivables": o.Receivables, "payables": o.Payables, "deposits": o.Deposits,
		"net_position": o.NetPosition, "month": mapMonth(o.Month), "month_margin_bps": o.MonthMarginBPS,
		"trend": trend, "exposure": exposure,
		"alerts": map[string]any{
			"low_deposits": a.LowDeposits, "low_accounts": a.LowAccounts, "unmatched_credits": a.UnmatchedCredits,
			"pending_refunds": a.PendingRefunds, "suspended_agencies": a.SuspendedAgencies,
			"overdue_schedules": a.OverdueSchedules, "supplier_due_soon": a.SupplierDueSoon,
		},
		"as_of": ts(o.AsOf),
	}
}

func mapAccount(a *domain.Account) map[string]any {
	return map[string]any{
		"id": a.ID, "branch_id": a.BranchID, "kind": a.Kind, "name": a.Name, "currency": a.Currency,
		"bank_name": a.BankName, "iban": a.IBAN, "commission_bps": a.CommissionBPS, "balance": a.Balance,
		"low_balance_threshold": a.LowBalanceThreshold, "low_balance": a.LowBalance(), "is_active": a.IsActive,
		"created_at": ts(a.CreatedAt), "updated_at": ts(a.UpdatedAt),
	}
}

func mapMovement(m *domain.Movement) map[string]any {
	return map[string]any{
		"id": m.ID, "account_id": m.AccountID, "branch_id": m.BranchID, "direction": m.Direction, "kind": m.Kind,
		"amount": m.Amount, "fee": m.Fee, "net": m.Net(), "currency": m.Currency, "balance_after": m.BalanceAfter,
		"booking_id": optID(m.BookingID), "supplier_id": optID(m.SupplierID),
		"counter_account_id": optID(m.CounterAccountID), "transfer_id": optID(m.TransferID),
		"source": m.Source, "external_id": m.ExternalID, "reference": m.Reference, "counterparty": m.Counterparty,
		"note": m.Note, "match_status": m.MatchStatus, "matched_payment_id": optID(m.MatchedPaymentID),
		"occurred_on": day(m.OccurredOn), "actor_id": optID(m.ActorID), "created_at": ts(m.CreatedAt),
	}
}

func mapMovements(items []domain.Movement) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, mapMovement(&items[i]))
	}
	return out
}

func mapAgency(a *domain.Agency) map[string]any {
	return map[string]any{
		"id": a.ID, "branch_id": a.BranchID, "code": a.Code, "name": a.Name, "contact_name": a.ContactName,
		"phone": a.Phone, "email": a.Email, "tax_id": a.TaxID, "currency": a.Currency, "credit_limit": a.CreditLimit,
		"payment_terms_days": a.PaymentTermsDays, "grace_days": a.GraceDays, "auto_suspend": a.AutoSuspend,
		"status": a.Status, "suspend_reason": a.SuspendReason, "created_at": ts(a.CreatedAt), "updated_at": ts(a.UpdatedAt),
	}
}

func mapAgencyView(v *app.AgencyView) map[string]any {
	m := mapAgency(&v.Agency)
	m["exposure"] = map[string]any{
		"outstanding": v.Exposure.Outstanding, "overdue": v.Exposure.Overdue,
		"oldest_overdue_days": v.Exposure.OldestOverdueDays, "open_bookings": v.Exposure.OpenBookings,
		"unconverted_balance": v.Exposure.UnconvertedBalance,
	}
	m["risk"] = map[string]any{"available": v.Risk.Available, "used_pct": v.Risk.UsedPct, "level": v.Risk.Level}
	return m
}

func mapReceivables(rc *app.Receivables) map[string]any {
	ageing := make([]map[string]any, 0, len(rc.Ageing))
	for _, a := range rc.Ageing {
		ageing = append(ageing, map[string]any{
			"currency": a.Currency, "buckets": a.Buckets, "counts": a.Counts, "total": a.Total(), "overdue": a.Overdue(),
		})
	}
	debtors := make([]map[string]any, 0, len(rc.Debtors))
	for _, d := range rc.Debtors {
		debtors = append(debtors, map[string]any{
			"booking_id": d.BookingID, "ref_no": d.RefNo, "customer_name": d.CustomerName, "phone": d.Phone,
			"agency_id": optID(d.AgencyID), "agency_name": d.AgencyName, "currency": d.Currency,
			"balance": d.Balance, "due_on": optDay(d.DueOn), "days_late": d.DaysLate,
		})
	}
	agencies := make([]map[string]any, 0, len(rc.Agencies))
	for i := range rc.Agencies {
		agencies = append(agencies, mapAgencyView(&rc.Agencies[i]))
	}
	return map[string]any{"ageing": ageing, "debtors": debtors, "agencies": agencies}
}

func mapSupplier(s *supplierdomain.Summary) map[string]any {
	f := s.Supplier.Finance
	return map[string]any{
		"id": s.Supplier.ID, "code": s.Supplier.Code, "name": s.Supplier.NameEn, "category": s.Supplier.Category,
		"is_active": s.Supplier.IsActive, "payment_model": f.Model, "currency": f.Currency,
		"deposit_balance": f.DepositBalance, "credit_limit": f.CreditLimit, "credit_used": f.CreditUsed,
		"low_balance_threshold": f.LowBalanceThreshold, "payment_terms": f.PaymentTerms,
		"open_disputes": s.OpenDisputes, "spend": s.Spend, "bookings": s.Bookings,
	}
}

func mapPayables(p *app.Payables) map[string]any {
	suppliers := make([]map[string]any, 0, len(p.Suppliers))
	for i := range p.Suppliers {
		suppliers = append(suppliers, mapSupplier(&p.Suppliers[i]))
	}
	plan := make([]map[string]any, 0, len(p.Plan))
	for _, d := range p.Plan {
		plan = append(plan, map[string]any{
			"id": d.ID, "supplier_id": d.SupplierID, "supplier_name": d.SupplierName, "invoice_number": d.InvoiceNumber,
			"status": d.Status, "currency": d.Currency, "amount": d.Amount, "due_on": optDay(d.DueOn),
		})
	}
	return map[string]any{"suppliers": suppliers, "plan": plan, "today": day(p.Today)}
}

func mapBudget(b *domain.Budget) any {
	if b == nil {
		return nil
	}
	return map[string]any{
		"departure_id": b.DepartureID, "currency": b.Currency, "revenue": b.Revenue, "cost": b.Cost,
		"margin": b.Margin(), "note": b.Note,
	}
}

func mapProfitability(p *app.Profitability) map[string]any {
	bookings := make([]map[string]any, 0, len(p.Bookings))
	for _, b := range p.Bookings {
		bookings = append(bookings, map[string]any{
			"booking_id": b.BookingID, "ref_no": b.RefNo, "customer_name": b.CustomerName, "owner_name": b.OwnerName,
			"service_type": b.ServiceType, "status": b.Status, "currency": b.Currency, "pnl": mapPnL(b.PnL),
			"created_at": ts(b.CreatedAt),
		})
	}
	departures := make([]map[string]any, 0, len(p.Departures))
	for _, d := range p.Departures {
		m := map[string]any{
			"departure_id": d.DepartureID, "package_name": d.PackageName, "departs_on": optDay(d.DepartsOn),
			"currency": d.Currency, "bookings": d.Bookings, "pax": d.Pax, "pnl": mapPnL(d.PnL), "budget": mapBudget(d.Budget),
			"variance": nil,
		}
		if d.Budget != nil && d.Budget.Currency == d.Currency {
			v := domain.VarianceOf(*d.Budget, d.PnL)
			m["variance"] = map[string]any{"revenue": v.Revenue, "cost": v.Cost, "margin": v.Margin, "cost_overrun": v.CostOverrun}
		}
		departures = append(departures, m)
	}
	reps := make([]map[string]any, 0, len(p.Reps))
	for _, r := range p.Reps {
		reps = append(reps, map[string]any{
			"user_id": r.UserID, "name": r.Name, "currency": r.Currency, "bookings": r.Bookings, "uncosted": r.Uncosted,
			"pnl": mapPnL(r.PnL), "commission": r.Commission,
		})
	}
	return map[string]any{
		"from": day(p.From), "to": day(p.To.AddDate(0, 0, -1)), "commission_bps": p.CommissionBPS,
		"bookings": bookings, "departures": departures, "reps": reps,
	}
}

func mapSettings(s app.Settings) map[string]any {
	return map[string]any{"reporting_currency": s.ReportingCurrency, "commission_bps": s.CommissionBPS}
}

func mapStatement(s *domain.BSPStatement) map[string]any {
	return map[string]any{
		"id": s.ID, "branch_id": s.BranchID, "label": s.Label, "period_start": day(s.PeriodStart),
		"period_end": day(s.PeriodEnd), "currency": s.Currency, "total": s.Total, "system_total": s.SystemTotal,
		"difference": s.Total - s.SystemTotal, "line_count": s.LineCount, "matched": s.Matched,
		"mismatched": s.Mismatched, "missing_system": s.MissingSystem, "missing_bsp": s.MissingBSP,
		"created_by": optID(s.CreatedBy), "created_at": ts(s.CreatedAt),
	}
}

func mapStatementDetail(s *domain.BSPStatement, lines []domain.BSPLine) map[string]any {
	m := mapStatement(s)
	out := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		var sys any
		if l.SystemAmount != nil {
			sys = *l.SystemAmount
		}
		out = append(out, map[string]any{
			"id": l.ID, "document_no": l.DocumentNo, "pnr": l.PNR, "type": l.Type, "passenger": l.Passenger,
			"issued_on": optDay(l.IssuedOn), "amount": l.Amount, "booking_id": optID(l.BookingID),
			"system_amount": sys, "status": l.Status,
		})
	}
	m["lines"] = out
	return m
}

func mapLetter(l *domain.Letter, now time.Time) map[string]any {
	return map[string]any{
		"id": l.ID, "branch_id": l.BranchID, "party_type": l.PartyType, "party_id": l.PartyID,
		"party_name": l.PartyName, "period_end": day(l.PeriodEnd), "balance": l.Balance, "currency": l.Currency,
		"email": l.Email, "status": l.StatusOn(now), "response_note": l.ResponseNote, "responded_by": l.RespondedBy,
		"responded_at": optTS(l.RespondedAt), "expires_at": ts(l.ExpiresAt), "created_at": ts(l.CreatedAt),
	}
}

// mapPublicLetter omits internal identifiers: the token holder only needs
// what the letter says.
func mapPublicLetter(p *app.PublicLetter, now time.Time) map[string]any {
	l := p.Letter
	return map[string]any{
		"company": p.Company, "party_type": l.PartyType, "party_name": l.PartyName, "period_end": day(l.PeriodEnd),
		"balance": l.Balance, "currency": l.Currency, "status": l.StatusOn(now), "response_note": l.ResponseNote,
		"responded_by": l.RespondedBy, "responded_at": optTS(l.RespondedAt), "expires_at": ts(l.ExpiresAt),
	}
}

func mapRefundQuote(q *app.RefundQuote) map[string]any {
	s := q.Settlement
	return map[string]any{
		"booking_id": optID(q.BookingID), "currency": q.Currency,
		"input": map[string]any{
			"paid": q.Input.Paid, "supplier_cost": q.Input.SupplierCost,
			"supplier_penalty": q.Input.SupplierPenalty, "service_fee": q.Input.ServiceFee,
		},
		"settlement": map[string]any{
			"customer_refund": s.CustomerRefund, "supplier_refund": s.SupplierRefund, "retained": s.Retained,
			"agency_result": s.AgencyResult, "shortfall": s.Shortfall,
		},
	}
}
