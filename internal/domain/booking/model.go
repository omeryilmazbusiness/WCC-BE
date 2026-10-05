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

	// RefNo is the database-assigned sequence behind RefCode.
	RefNo          int64
	PNR            string
	ServiceType    string
	SupplierSource string
	Channel        string
	Summary        string
	CompanyName    string
	// ReissueCount counts completed change requests.
	ReissueCount int

	// Info is read-only context joined in by the repository.
	Info Info
}

// Info is display context resolved from related records at read time.
type Info struct {
	CustomerName      string
	CustomerNameAr    string
	OwnerName         string
	PackageID         *uuid.UUID
	PackageCode       string
	PackageName       string
	PackageNameAr     string
	PackageKind       string
	DepartureCode     string
	DepartDate        *time.Time
	ReturnDate        *time.Time
	MakkahHotel       string
	MadinahHotel      string
	FlightRouting     string
	ParticipantsCount int
	RefundedAmt       int64
	OverdueSchedule   bool
	VisaPending       int
	OpenChanges       int
}

// TicketStatus is the derived fulfilment status.
func (b *Booking) TicketStatus() string {
	return TicketStatus(b.Status, b.ReissueCount, b.Info.RefundedAmt)
}

// PaymentStatus is the derived collection status.
func (b *Booking) PaymentStatus() string {
	return PaymentStatus(b.TotalAmount, b.CollectedAmt, b.BalanceAmt, b.Info.OverdueSchedule)
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
	Gender      string
	// NationalID (e.g. TCKN) is write-only: set it to store a new number,
	// leave it blank to keep the stored one. Reads fill NationalIDLast4 only.
	NationalID      string
	NationalIDLast4 string
	// HealthOK records that the vaccine / health form was verified.
	HealthOK  bool
	CreatedAt time.Time
}

const (
	GenderMale   = "male"
	GenderFemale = "female"
)

// ValidGender accepts male, female or blank (not recorded).
func ValidGender(g string) bool {
	return g == "" || g == GenderMale || g == GenderFemale
}

const MaxNationalIDLen = 20

// ValidNationalID accepts blank (keep) or 5–20 letters and digits.
func ValidNationalID(id string) bool {
	if id == "" {
		return true
	}
	if len(id) < 5 || len(id) > MaxNationalIDLen {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
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
	// Query matches PNR, booking reference, customer name/phone/email,
	// passenger names and (exactly) passport numbers.
	Query       string
	ServiceType string
	Channel     string
	Segment     Segment
	// DateField selects which date From/To bound: created, depart or return.
	DateField DateField
	From      *time.Time
	To        *time.Time
	// Now and DayEnd anchor time-relative segments (option_today).
	Now    time.Time
	DayEnd time.Time
	Sort   string
	Limit  int
	Offset int
}

// Segment is a quick operations filter.
type Segment string

const (
	SegmentAll         Segment = ""
	SegmentOptionToday Segment = "option_today"
	SegmentPaymentDue  Segment = "payment_due"
	SegmentVisaPending Segment = "visa_pending"
	SegmentOverdue     Segment = "overdue"
	SegmentIssued      Segment = "issued"
	SegmentCancelled   Segment = "cancelled"
)

func (s Segment) Valid() bool {
	switch s {
	case SegmentAll, SegmentOptionToday, SegmentPaymentDue, SegmentVisaPending, SegmentOverdue, SegmentIssued, SegmentCancelled:
		return true
	}
	return false
}

type DateField string

const (
	DateCreated DateField = "created"
	DateDepart  DateField = "depart"
	DateReturn  DateField = "return"
)

func (d DateField) Valid() bool {
	return d == DateCreated || d == DateDepart || d == DateReturn
}

const (
	SortRecent = "recent"
	SortTTL    = "ttl"
	SortDepart = "depart"
)

func ValidSort(s string) bool {
	return s == "" || s == SortRecent || s == SortTTL || s == SortDepart
}

// Stats are segment counts for the operations header.
type Stats struct {
	Active       int `json:"active"`
	OptionToday  int `json:"option_today"`
	OptionUrgent int `json:"option_urgent"`
	PaymentDue   int `json:"payment_due"`
	Overdue      int `json:"overdue"`
	VisaPending  int `json:"visa_pending"`
	Issued       int `json:"issued"`
	Cancelled    int `json:"cancelled"`
	Total        int `json:"total"`
}

// UrgentHoldWindow marks options expiring this soon as urgent.
const UrgentHoldWindow = 2 * time.Hour

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
