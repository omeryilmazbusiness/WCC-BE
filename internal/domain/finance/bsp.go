package finance

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// BSP line statuses after the cross-check.
const (
	BSPMatched         = "matched"
	BSPAmountMismatch  = "amount_mismatch"
	BSPMissingInSystem = "missing_in_system"
	BSPMissingInBSP    = "missing_in_bsp"
)

// BSP document types.
const (
	BSPSale   = "sale"
	BSPRefund = "refund"
	BSPADM    = "adm"
	BSPACM    = "acm"
)

var BSPTypes = []string{BSPSale, BSPRefund, BSPADM, BSPACM}

// MaxBSPLines bounds one imported statement.
const MaxBSPLines = 5_000

// BSPLine is one document on an airline billing statement, or a system
// ticket the statement did not contain (missing_in_bsp).
type BSPLine struct {
	ID           uuid.UUID
	StatementID  uuid.UUID
	DocumentNo   string
	PNR          string
	Type         string
	Passenger    string
	IssuedOn     *time.Time
	Amount       int64
	BookingID    *uuid.UUID
	SystemAmount *int64
	Status       string
}

// SignedAmount is what the agency remits: sales and ADMs are owed, refunds
// and ACMs are credited.
func (l BSPLine) SignedAmount() int64 {
	if l.Type == BSPRefund || l.Type == BSPACM {
		return -l.Amount
	}
	return l.Amount
}

// BSPStatement is one imported billing period.
type BSPStatement struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	Label         string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Currency      string
	Total         int64
	SystemTotal   int64
	LineCount     int
	Matched       int
	Mismatched    int
	MissingSystem int
	MissingBSP    int
	CreatedBy     *uuid.UUID
	CreatedAt     time.Time
}

// SystemTicket is a booking the agency expects on the statement: an
// air booking with a PNR whose cost is the net remittance.
type SystemTicket struct {
	BookingID uuid.UUID
	PNR       string
	Amount    int64
	Currency  string
}

// NormalizeLines validates imported lines.
func NormalizeLines(lines []BSPLine) error {
	if len(lines) == 0 || len(lines) > MaxBSPLines {
		return fieldErr("lines", "1-5000 lines")
	}
	f := fields{}
	for i := range lines {
		l := &lines[i]
		l.DocumentNo = strings.TrimSpace(l.DocumentNo)
		l.PNR = strings.ToUpper(strings.TrimSpace(l.PNR))
		l.Type = strings.ToLower(strings.TrimSpace(l.Type))
		l.Passenger = strings.TrimSpace(l.Passenger)
		if l.Type == "" {
			l.Type = BSPSale
		}
		if l.DocumentNo == "" || len(l.DocumentNo) > 20 || len(l.PNR) > 12 || len(l.Passenger) > 120 ||
			!slices.Contains(BSPTypes, l.Type) || l.Amount < 0 || l.Amount > MaxMoney {
			f.add(fmt.Sprintf("lines.%d", i), "document number, type and a non-negative amount are required")
		}
	}
	return f.err("invalid statement lines")
}

// Reconcile cross-checks statement lines against system tickets by PNR.
// Lines sharing a PNR (one ticket per passenger) are summed before the
// comparison; tickets in the period with no statement line come back as
// missing_in_bsp lines. Lines are updated in place.
func Reconcile(st *BSPStatement, lines []BSPLine, tickets []SystemTicket) []BSPLine {
	byPNR := map[string]SystemTicket{}
	for _, t := range tickets {
		p := strings.ToUpper(strings.TrimSpace(t.PNR))
		if p == "" || t.Currency != st.Currency {
			continue
		}
		if prev, ok := byPNR[p]; ok {
			prev.Amount += t.Amount
			byPNR[p] = prev
			continue
		}
		t.PNR = p
		byPNR[p] = t
	}
	sums := map[string]int64{}
	for _, l := range lines {
		if l.PNR != "" {
			sums[l.PNR] += l.SignedAmount()
		}
	}
	st.Total, st.SystemTotal = 0, 0
	st.Matched, st.Mismatched, st.MissingSystem, st.MissingBSP = 0, 0, 0, 0
	seen := map[string]bool{}
	for i := range lines {
		l := &lines[i]
		st.Total += l.SignedAmount()
		t, ok := byPNR[l.PNR]
		if l.PNR == "" || !ok {
			l.Status, l.BookingID, l.SystemAmount = BSPMissingInSystem, nil, nil
			st.MissingSystem++
			continue
		}
		id, amt := t.BookingID, t.Amount
		l.BookingID, l.SystemAmount = &id, &amt
		if sums[l.PNR] == t.Amount {
			l.Status = BSPMatched
			st.Matched++
		} else {
			l.Status = BSPAmountMismatch
			st.Mismatched++
		}
		seen[l.PNR] = true
	}
	pnrs := make([]string, 0, len(byPNR))
	for p := range byPNR {
		pnrs = append(pnrs, p)
	}
	slices.Sort(pnrs)
	for _, p := range pnrs {
		t := byPNR[p]
		st.SystemTotal += t.Amount
		if seen[p] {
			continue
		}
		id, amt := t.BookingID, t.Amount
		lines = append(lines, BSPLine{
			ID: uuid.New(), StatementID: st.ID, PNR: p, Type: BSPSale,
			BookingID: &id, SystemAmount: &amt, Status: BSPMissingInBSP,
		})
		st.MissingBSP++
	}
	st.LineCount = len(lines)
	return lines
}

// Discrepancy is the statement total minus what the system expected.
func (st BSPStatement) Discrepancy() int64 { return st.Total - st.SystemTotal }
