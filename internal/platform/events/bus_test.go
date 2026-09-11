package events_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
)

func TestBusPublishFanOutAndIsolatesErrors(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	bus := events.NewBus(log)

	var okCount, failCount atomic.Int32
	bus.Subscribe(events.LeadCreated, func(ctx context.Context, event events.Event) error {
		okCount.Add(1)
		return nil
	})
	bus.Subscribe(events.LeadCreated, func(ctx context.Context, event events.Event) error {
		failCount.Add(1)
		return errors.New("boom")
	})
	bus.Subscribe(events.LeadCreated, func(ctx context.Context, event events.Event) error {
		okCount.Add(1)
		return nil
	})

	bus.Publish(context.Background(), events.Event{Name: events.LeadCreated, Payload: "x"})
	if okCount.Load() != 2 || failCount.Load() != 1 {
		t.Fatalf("ok=%d fail=%d", okCount.Load(), failCount.Load())
	}
}

func TestBusIgnoresUnknownEvents(t *testing.T) {
	bus := events.NewBus(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	var n atomic.Int32
	bus.Subscribe(events.PaymentRecorded, func(ctx context.Context, event events.Event) error {
		n.Add(1)
		return nil
	})
	bus.Publish(context.Background(), events.Event{Name: events.BookingDrafted})
	if n.Load() != 0 {
		t.Fatal("unexpected handler call")
	}
}
