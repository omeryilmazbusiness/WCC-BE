package booking

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Line categories describe what an item line sells.
const (
	LinePackage   = "package"
	LineHotel     = "hotel"
	LineRoom      = "room"
	LineTransport = "transport"
	LineFlight    = "flight"
	LineExtras    = "extras"
)

func ValidLineCategory(c string) bool {
	switch c {
	case LinePackage, LineHotel, LineRoom, LineTransport, LineFlight, LineExtras:
		return true
	default:
		return false
	}
}

// Line kinds split a booking total into its financial components (T-274).
const (
	KindItem = "item"
	KindTax  = "tax"
	KindFee  = "fee"
)

func ValidLineKind(k string) bool {
	return k == KindItem || k == KindTax || k == KindFee
}

type Booking struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	CustomerID   uuid.UUID
	DepartureID  uuid.UUID
	LeadID       *uuid.UUID
	Status       Status
	PaxCount     int
	TotalAmount  int64
	DiscountAmt  int64
	TaxAmt       int64
	FeeAmt       int64
	CostAmt      int64
	CollectedAmt int64
	BalanceAmt   int64
	Currency     string
	Notes        string
	OwnerID      uuid.UUID
	// HoldExpiresAt is set exactly while Status is option_hold.
	HoldExpiresAt   *time.Time
	StatusChangedAt time.Time
	StatusReason    string
	// ReadyForced keeps an overridden ready status from being re-derived.
	ReadyForced bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Subtotal is the item amount before discount, tax and fees.
func (b *Booking) Subtotal() int64 {
	return b.TotalAmount + b.DiscountAmt - b.TaxAmt - b.FeeAmt
}

// Margin is net revenue after discount minus cost; tax and fees are pass-through.
func (b *Booking) Margin() int64 {
	return b.TotalAmount - b.TaxAmt - b.FeeAmt - b.CostAmt
}

type Participant struct {
	ID          uuid.UUID
	BookingID   uuid.UUID
	FullName    string
	PassportNo  string
	Nationality string
	DateOfBirth *time.Time
	CreatedAt   time.Time
}

func (p *Participant) PassportMissing() bool {
	return len(p.PassportNo) == 0
}

type LineItem struct {
	ID        uuid.UUID
	BookingID uuid.UUID
	Kind      string
	// Category is set for item lines only.
	Category  string
	Label     string
	Quantity  int
	UnitPrice int64
	UnitCost  int64
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (l *LineItem) LineTotal() int64 { return int64(l.Quantity) * l.UnitPrice }
func (l *LineItem) LineCost() int64  { return int64(l.Quantity) * l.UnitCost }

type ChecklistItem struct {
	ID          uuid.UUID
	BookingID   uuid.UUID
	Code        string
	Label       string
	Required    bool
	Completed   bool
	CompletedAt *time.Time
	SortOrder   int
	CreatedAt   time.Time
}

type ListFilter struct {
	BranchID    *uuid.UUID
	CustomerID  *uuid.UUID
	DepartureID *uuid.UUID
	PackageID   *uuid.UUID
	OwnerID     *uuid.UUID
	LeadID      *uuid.UUID
	Status      Status
	Query       string
	Limit       int
	Offset      int
}

// Readiness reports the travel-readiness gate for the ready status and
// whether a manual confirm would currently pass its guards.
type Readiness struct {
	BookingID  uuid.UUID `json:"booking_id"`
	CanConfirm bool      `json:"can_confirm"`
	// ConfirmGuards lists the guards a confirm request would fail now.
	ConfirmGuards []Guard `json:"confirm_guards"`
	// ReadinessOK is the readiness input of the derived ready status.
	ReadinessOK         bool     `json:"readiness_ok"`
	Blocking            []string `json:"blocking"`
	Warnings            []string `json:"warnings"`
	ParticipantsCount   int      `json:"participants_count"`
	PaxCount            int      `json:"pax_count"`
	MissingPassports    int      `json:"missing_passports"`
	ChecklistRequired   int      `json:"checklist_required"`
	ChecklistCompleted  int      `json:"checklist_completed"`
	ChecklistIncomplete int      `json:"checklist_incomplete"`
	BalanceAmt          int64    `json:"balance_amt"`
	DaysToDeparture     *int     `json:"days_to_departure,omitempty"`
	RiskAlerts          []string `json:"risk_alerts"`
	OverrideActive      bool     `json:"override_active"`
	MissingDocs         []string `json:"missing_docs"`
}

// ReadinessOverride lifts the checklist and document gates of readiness (T-271).
type ReadinessOverride struct {
	ID        uuid.UUID
	BookingID uuid.UUID
	Reason    string
	ActorID   *uuid.UUID
	CreatedAt time.Time
}

func (b *Booking) RecomputeBalance() {
	b.BalanceAmt = b.TotalAmount - b.CollectedAmt
	if b.BalanceAmt < 0 {
		b.BalanceAmt = 0
	}
}

// Totals is the deterministic breakdown of a line-driven booking:
// Total = Subtotal - Discount + Tax + Fees.
type Totals struct {
	Subtotal int64
	Discount int64
	Tax      int64
	Fees     int64
	Total    int64
	Cost     int64
}

// ComputeTotals sums lines by kind; the discount applies to item lines only.
func ComputeTotals(lines []LineItem, discount int64) (Totals, error) {
	if discount < 0 {
		return Totals{}, shared.NewValidation("discount_amt must be >= 0")
	}
	t := Totals{Discount: discount}
	for _, l := range lines {
		switch l.Kind {
		case KindItem:
			t.Subtotal += l.LineTotal()
		case KindTax:
			t.Tax += l.LineTotal()
		case KindFee:
			t.Fees += l.LineTotal()
		default:
			return Totals{}, shared.NewValidation("invalid line kind: " + l.Kind)
		}
		t.Cost += l.LineCost()
	}
	if discount > t.Subtotal {
		return Totals{}, shared.NewValidation("discount_amt cannot exceed the item subtotal")
	}
	t.Total = t.Subtotal - t.Discount + t.Tax + t.Fees
	return t, nil
}

func (b *Booking) applyTotals(t Totals, now time.Time) {
	b.DiscountAmt = t.Discount
	b.TaxAmt = t.Tax
	b.FeeAmt = t.Fees
	b.TotalAmount = t.Total
	b.CostAmt = t.Cost
	b.RecomputeBalance()
	b.UpdatedAt = now
}

// RecalculateFromLines derives the money fields from line items (T-062, T-274).
func (b *Booking) RecalculateFromLines(lines []LineItem, now time.Time) error {
	t, err := ComputeTotals(lines, b.DiscountAmt)
	if err != nil {
		return err
	}
	b.applyTotals(t, now)
	return nil
}

// UpdateFields is a PATCH of the editable booking fields.
type UpdateFields struct {
	PaxCount    int
	TotalAmount int64
	Currency    string
	DiscountAmt *int64
	Notes       *string
}

// ApplyUpdate edits a draft/quoted booking. With line items the money
// fields are derived from them and TotalAmount is ignored; without lines the
// legacy manual total (amount owed) is kept.
func (b *Booking) ApplyUpdate(in UpdateFields, lines []LineItem, now time.Time) error {
	if !b.Status.Editable() {
		return shared.NewInvalidState("only draft or quoted bookings can be updated")
	}
	if in.PaxCount <= 0 {
		return shared.NewValidation("pax_count must be > 0")
	}
	if in.TotalAmount < 0 {
		return shared.NewValidation("total_amount must be >= 0")
	}
	discount := b.DiscountAmt
	if in.DiscountAmt != nil {
		if *in.DiscountAmt < 0 {
			return shared.NewValidation("discount_amt must be >= 0")
		}
		discount = *in.DiscountAmt
	}
	if len(lines) > 0 {
		t, err := ComputeTotals(lines, discount)
		if err != nil {
			return err
		}
		b.applyTotals(t, now)
	} else {
		b.TotalAmount = in.TotalAmount
		b.DiscountAmt = discount
	}
	b.PaxCount = in.PaxCount
	if in.Currency != "" {
		b.Currency = in.Currency
	}
	if in.Notes != nil {
		b.Notes = *in.Notes
	}
	b.RecomputeBalance()
	b.UpdatedAt = now
	return nil
}

// DefaultChecklist returns travel readiness hooks seeded on draft create (T-065).
func DefaultChecklist(bookingID uuid.UUID) []ChecklistItem {
	now := time.Now().UTC()
	defs := []struct {
		code, label string
		required    bool
	}{
		{"passport", "Passport copy", true},
		{"visa", "Visa / entry permit", true},
		{"photo", "Passport photo", false},
		{"payment", "Deposit / payment plan", true},
		{"flight", "Flight confirmation", false},
		{"vaccine", "Health / vaccine form", false},
	}
	out := make([]ChecklistItem, 0, len(defs))
	for i, d := range defs {
		out = append(out, ChecklistItem{
			ID: uuid.New(), BookingID: bookingID, Code: d.code, Label: d.label,
			Required: d.required, SortOrder: i, CreatedAt: now,
		})
	}
	return out
}

type Repository interface {
	Create(ctx context.Context, b *Booking) error
	Update(ctx context.Context, b *Booking) error
	FindByID(ctx context.Context, id uuid.UUID) (*Booking, error)
	List(ctx context.Context, f ListFilter) ([]Booking, int, error)

	AddParticipant(ctx context.Context, p *Participant) error
	UpdateParticipant(ctx context.Context, p *Participant) error
	DeleteParticipant(ctx context.Context, bookingID, participantID uuid.UUID) error
	ListParticipants(ctx context.Context, bookingID uuid.UUID) ([]Participant, error)

	CountConfirmedPaxByDeparture(ctx context.Context, departureID uuid.UUID) (int, error)
	ListByDeparture(ctx context.Context, departureID uuid.UUID) ([]Booking, error)

	ReplaceLineItems(ctx context.Context, bookingID uuid.UUID, items []LineItem) error
	ListLineItems(ctx context.Context, bookingID uuid.UUID) ([]LineItem, error)

	SeedChecklist(ctx context.Context, items []ChecklistItem) error
	ListChecklist(ctx context.Context, bookingID uuid.UUID) ([]ChecklistItem, error)
	UpdateChecklistItem(ctx context.Context, item *ChecklistItem) error

	UpsertReadinessOverride(ctx context.Context, o *ReadinessOverride) error
	FindReadinessOverride(ctx context.Context, bookingID uuid.UUID) (*ReadinessOverride, error)
}

// LifecycleStore is the persistence port of status transitions and sweeps.
// Status columns are written only through SaveStatus, so generic updates
// (e.g. payment balance sync) can never overwrite a lifecycle change.
type LifecycleStore interface {
	// FindForUpdate loads the booking and row-locks it for the transaction.
	FindForUpdate(ctx context.Context, id uuid.UUID) (*Booking, error)
	SaveStatus(ctx context.Context, b *Booking) error
	// LockDeparture serializes seat accounting on one departure.
	LockDeparture(ctx context.Context, departureID uuid.UUID) error
	// The List* sweeps page by id: they return ids greater than after.
	ListExpiredHolds(ctx context.Context, now time.Time, after uuid.UUID, limit int) ([]uuid.UUID, error)
	ListDueForTravel(ctx context.Context, today time.Time, after uuid.UUID, limit int) ([]uuid.UUID, error)
	ListDerivedCandidates(ctx context.Context, after uuid.UUID, limit int) ([]uuid.UUID, error)
}
