package task_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	leaddomain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type taskMem struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*domain.Task
	byKey map[string]*domain.Task
}

func newTaskMem() *taskMem {
	return &taskMem{byID: map[uuid.UUID]*domain.Task{}, byKey: map[string]*domain.Task{}}
}

func (m *taskMem) Create(_ context.Context, t *domain.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.byID[t.ID] = &cp
	if t.IdempotencyKey != "" {
		m.byKey[t.IdempotencyKey] = &cp
	}
	return nil
}

func (m *taskMem) Update(_ context.Context, t *domain.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.byID[t.ID] = &cp
	if t.IdempotencyKey != "" {
		m.byKey[t.IdempotencyKey] = &cp
	}
	return nil
}

func (m *taskMem) FindByID(_ context.Context, id uuid.UUID) (*domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[id]
	if !ok {
		return nil, fmt.Errorf("missing")
	}
	cp := *t
	return &cp, nil
}

func (m *taskMem) FindByIdempotencyKey(_ context.Context, key string) (*domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byKey[key]
	if !ok {
		return nil, fmt.Errorf("missing")
	}
	cp := *t
	return &cp, nil
}

func (m *taskMem) ListByAssignee(context.Context, uuid.UUID, *domain.Status, int, int) ([]domain.Task, int, error) {
	return nil, 0, nil
}

func (m *taskMem) ListByRelated(_ context.Context, relatedType string, relatedID uuid.UUID) ([]domain.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Task
	for _, t := range m.byID {
		if t.RelatedType == relatedType && t.RelatedID == relatedID {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (m *taskMem) CountOverdue(context.Context, *uuid.UUID) (int, error) { return 0, nil }

type bookingMem struct {
	byID map[uuid.UUID]*bookingdomain.Booking
}

func (m *bookingMem) Create(context.Context, *bookingdomain.Booking) error { return nil }
func (m *bookingMem) Update(_ context.Context, b *bookingdomain.Booking) error {
	cp := *b
	m.byID[b.ID] = &cp
	return nil
}
func (m *bookingMem) FindByID(_ context.Context, id uuid.UUID) (*bookingdomain.Booking, error) {
	b, ok := m.byID[id]
	if !ok {
		return nil, fmt.Errorf("missing")
	}
	cp := *b
	return &cp, nil
}
func (m *bookingMem) List(context.Context, bookingdomain.ListFilter) ([]bookingdomain.Booking, int, error) {
	return nil, 0, nil
}
func (m *bookingMem) AddParticipant(context.Context, *bookingdomain.Participant) error { return nil }
func (m *bookingMem) UpdateParticipant(context.Context, *bookingdomain.Participant) error {
	return nil
}
func (m *bookingMem) DeleteParticipant(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (m *bookingMem) ListParticipants(context.Context, uuid.UUID) ([]bookingdomain.Participant, error) {
	return nil, nil
}
func (m *bookingMem) CountConfirmedPaxByDeparture(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}
func (m *bookingMem) ListByDeparture(context.Context, uuid.UUID) ([]bookingdomain.Booking, error) {
	return nil, nil
}
func (m *bookingMem) ReplaceLineItems(context.Context, uuid.UUID, []bookingdomain.LineItem) error {
	return nil
}
func (m *bookingMem) ListLineItems(context.Context, uuid.UUID) ([]bookingdomain.LineItem, error) {
	return nil, nil
}
func (m *bookingMem) SeedChecklist(context.Context, []bookingdomain.ChecklistItem) error { return nil }
func (m *bookingMem) ListChecklist(context.Context, uuid.UUID) ([]bookingdomain.ChecklistItem, error) {
	return nil, nil
}
func (m *bookingMem) UpdateChecklistItem(context.Context, *bookingdomain.ChecklistItem) error {
	return nil
}

func TestReactorPaymentClosesTasksWhenBalanceZero(t *testing.T) {
	tasks := newTaskMem()
	bookingID := uuid.New()
	now := time.Now().UTC()
	_ = tasks.Create(context.Background(), &domain.Task{
		ID: uuid.New(), BranchID: uuid.New(), Title: "Collect payment", Kind: domain.KindPayment,
		Status: domain.StatusOpen, AssigneeID: uuid.New(), RelatedType: "booking", RelatedID: bookingID,
		CreatedAt: now, UpdatedAt: now,
	})
	bookings := &bookingMem{byID: map[uuid.UUID]*bookingdomain.Booking{
		bookingID: {ID: bookingID, BalanceAmt: 0, Status: bookingdomain.StatusConfirmed},
	}}
	bus := events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	reactor := apptask.NewReactor(tasks, bookings, tx.Nop{})
	reactor.Register(bus)

	bus.Publish(context.Background(), events.Event{
		Name:    events.PaymentRecorded,
		Payload: &paymentdomain.Payment{ID: uuid.New(), BookingID: bookingID, Amount: 100},
	})

	items, _ := tasks.ListByRelated(context.Background(), "booking", bookingID)
	if len(items) != 1 || items[0].Status != domain.StatusDone {
		t.Fatalf("expected payment task done, got %#v", items)
	}
}

func TestReactorPaymentKeepsTasksWhenBalanceRemains(t *testing.T) {
	tasks := newTaskMem()
	bookingID := uuid.New()
	now := time.Now().UTC()
	_ = tasks.Create(context.Background(), &domain.Task{
		ID: uuid.New(), Kind: domain.KindPayment, Status: domain.StatusOpen,
		RelatedType: "booking", RelatedID: bookingID, CreatedAt: now, UpdatedAt: now,
	})
	bookings := &bookingMem{byID: map[uuid.UUID]*bookingdomain.Booking{
		bookingID: {ID: bookingID, BalanceAmt: 500},
	}}
	bus := events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	apptask.NewReactor(tasks, bookings, tx.Nop{}).Register(bus)
	bus.Publish(context.Background(), events.Event{
		Name: events.PaymentRecorded, Payload: &paymentdomain.Payment{BookingID: bookingID},
	})
	items, _ := tasks.ListByRelated(context.Background(), "booking", bookingID)
	if items[0].Status != domain.StatusOpen {
		t.Fatalf("expected still open, got %s", items[0].Status)
	}
}

func TestReactorCancelAndLeadConvertedIdempotent(t *testing.T) {
	tasks := newTaskMem()
	bookingID := uuid.New()
	leadID := uuid.New()
	now := time.Now().UTC()
	_ = tasks.Create(context.Background(), &domain.Task{
		ID: uuid.New(), Kind: domain.KindDocument, Status: domain.StatusOpen,
		RelatedType: "booking", RelatedID: bookingID, CreatedAt: now, UpdatedAt: now,
	})
	bookings := &bookingMem{byID: map[uuid.UUID]*bookingdomain.Booking{}}
	bus := events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	reactor := apptask.NewReactor(tasks, bookings, tx.Nop{})
	reactor.Register(bus)

	bus.Publish(context.Background(), events.Event{
		Name: events.BookingCancelled, Payload: &bookingdomain.Booking{ID: bookingID},
	})
	items, _ := tasks.ListByRelated(context.Background(), "booking", bookingID)
	if items[0].Status != domain.StatusCancelled {
		t.Fatalf("expected cancelled, got %s", items[0].Status)
	}

	lead := &leaddomain.Lead{ID: leadID, BranchID: uuid.New(), OwnerID: uuid.New(), Stage: leaddomain.StageWon}
	bus.Publish(context.Background(), events.Event{Name: events.LeadConverted, Payload: lead})
	bus.Publish(context.Background(), events.Event{Name: events.LeadConverted, Payload: lead})

	key := fmt.Sprintf("lead:%s:converted-booking", leadID)
	t1, err := tasks.FindByIdempotencyKey(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	// Second publish must not duplicate.
	count := 0
	for _, tk := range tasks.byID {
		if tk.IdempotencyKey == key {
			count++
		}
	}
	if count != 1 || t1.Kind != domain.KindCustom {
		t.Fatalf("idempotency failed count=%d task=%#v", count, t1)
	}
}

func TestSeederBookingConfirmedIdempotent(t *testing.T) {
	tasks := newTaskMem()
	bus := events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	seeder := apptask.NewSeeder(tasks, tx.Nop{})
	seeder.Register(bus)

	b := &bookingdomain.Booking{ID: uuid.New(), BranchID: uuid.New(), OwnerID: uuid.New()}
	bus.Publish(context.Background(), events.Event{Name: events.BookingConfirmed, Payload: b})
	bus.Publish(context.Background(), events.Event{Name: events.BookingConfirmed, Payload: b})

	items, _ := tasks.ListByRelated(context.Background(), "booking", b.ID)
	if len(items) != 2 {
		t.Fatalf("want 2 seeded tasks, got %d", len(items))
	}
}
