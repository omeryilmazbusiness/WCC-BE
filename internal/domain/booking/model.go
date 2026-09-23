package booking

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusConfirmed Status = "confirmed"
	StatusCancelled Status = "cancelled"
	StatusCompleted Status = "completed"
)

const (
	LinePackage   = "package"
	LineHotel     = "hotel"
	LineRoom      = "room"
	LineTransport = "transport"
	LineFlight    = "flight"
	LineExtras    = "extras"
)

func ValidLineKind(k string) bool {
	switch k {
	case LinePackage, LineHotel, LineRoom, LineTransport, LineFlight, LineExtras:
		return true
	default:
		return false
	}
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
	CostAmt      int64
	CollectedAmt int64
	BalanceAmt   int64
	Currency     string
	Notes        string
	OwnerID      uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (b *Booking) Margin() int64 {
	return b.TotalAmount - b.CostAmt - b.DiscountAmt
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
	ID         uuid.UUID
	BookingID  uuid.UUID
	Kind       string
	Label      string
	Quantity   int
	UnitPrice  int64
	UnitCost   int64
	SortOrder  int
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
	OwnerID     *uuid.UUID
	LeadID      *uuid.UUID
	Status      Status
	Query       string
	Limit       int
	Offset      int
}

// Readiness for confirm gate + travel checklist (T-065).
type Readiness struct {
	BookingID            uuid.UUID `json:"booking_id"`
	CanConfirm           bool      `json:"can_confirm"`
	Blocking             []string  `json:"blocking"`
	Warnings             []string  `json:"warnings"`
	ParticipantsCount    int       `json:"participants_count"`
	PaxCount             int       `json:"pax_count"`
	MissingPassports     int       `json:"missing_passports"`
	ChecklistRequired    int       `json:"checklist_required"`
	ChecklistCompleted   int       `json:"checklist_completed"`
	ChecklistIncomplete  int       `json:"checklist_incomplete"`
	BalanceAmt           int64     `json:"balance_amt"`
	DaysToDeparture      *int      `json:"days_to_departure,omitempty"`
	RiskAlerts           []string  `json:"risk_alerts"`
}

var allowed = map[Status][]Status{
	StatusDraft:     {StatusConfirmed, StatusCancelled},
	StatusConfirmed: {StatusCompleted, StatusCancelled},
	StatusCancelled: {},
	StatusCompleted: {},
}

func CanTransition(from, to Status) bool {
	for _, s := range allowed[from] {
		if s == to {
			return true
		}
	}
	return false
}

func (b *Booking) TransitionTo(to Status) error {
	if !CanTransition(b.Status, to) {
		return shared.NewInvalidState("cannot transition booking from " + string(b.Status) + " to " + string(to))
	}
	b.Status = to
	b.UpdatedAt = time.Now().UTC()
	return nil
}

func (b *Booking) RecomputeBalance() {
	b.BalanceAmt = b.TotalAmount - b.CollectedAmt
	if b.BalanceAmt < 0 {
		b.BalanceAmt = 0
	}
}

// RecalculateFromLines sets TotalAmount/CostAmt from line items minus discount (T-062).
func (b *Booking) RecalculateFromLines(lines []LineItem) {
	var total, cost int64
	for _, l := range lines {
		total += l.LineTotal()
		cost += l.LineCost()
	}
	if b.DiscountAmt < 0 {
		b.DiscountAmt = 0
	}
	b.TotalAmount = total - b.DiscountAmt
	if b.TotalAmount < 0 {
		b.TotalAmount = 0
	}
	b.CostAmt = cost
	b.RecomputeBalance()
	b.UpdatedAt = time.Now().UTC()
}

func (b *Booking) ApplyUpdate(pax int, total int64, currency string, discount *int64, notes *string) error {
	if b.Status != StatusDraft {
		return shared.NewInvalidState("only draft bookings can be updated")
	}
	if pax <= 0 {
		return shared.NewValidation("pax_count must be > 0")
	}
	if total < 0 {
		return shared.NewValidation("total_amount must be >= 0")
	}
	b.PaxCount = pax
	b.TotalAmount = total
	if currency != "" {
		b.Currency = currency
	}
	if discount != nil {
		if *discount < 0 {
			return shared.NewValidation("discount_amt must be >= 0")
		}
		b.DiscountAmt = *discount
	}
	if notes != nil {
		b.Notes = *notes
	}
	b.RecomputeBalance()
	b.UpdatedAt = time.Now().UTC()
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
}
