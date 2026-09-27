package queue_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/queue"
	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestMemoryEnqueueAndInspect(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	c, err := queue.NewClient(config.RedisConfig{URL: "memory://"}, log, false)
	if err != nil {
		t.Fatal(err)
	}

	id, err := c.Enqueue(context.Background(), shared.JobReminderSend, []byte(`{"task_id":"t1"}`), shared.EnqueueOpts{
		Queue:    queue.QueueDefault,
		MaxRetry: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	info, err := c.GetJob(context.Background(), queue.QueueDefault, id)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != string(shared.JobReminderSend) || info.State != "pending" {
		t.Fatalf("info=%+v", info)
	}
	stats, err := c.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending < 1 || stats.Mode != "memory" {
		t.Fatalf("stats=%+v", stats)
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

func TestStrictModeNeverFallsBackToMemory(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	if _, err := queue.NewClient(config.RedisConfig{URL: "memory://"}, log, true); err == nil {
		t.Fatal("strict mode must reject the memory queue")
	}
	// Nothing listens on port 1: strict keeps redis mode so readiness fails.
	c, err := queue.NewClient(config.RedisConfig{URL: "redis://127.0.0.1:1/0"}, log, true)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Mode() != "redis" {
		t.Fatalf("mode=%s, want redis", c.Mode())
	}
	if err := c.Ping(context.Background()); err == nil {
		t.Fatal("ping must fail while redis is down")
	}
	if _, err := c.Enqueue(context.Background(), shared.JobReminderSend, nil, shared.EnqueueOpts{}); err == nil {
		t.Fatal("enqueue must fail loudly instead of dropping the job")
	}
	lenient, err := queue.NewClient(config.RedisConfig{URL: "redis://127.0.0.1:1/0"}, log, false)
	if err != nil {
		t.Fatal(err)
	}
	if lenient.Mode() != "memory" {
		t.Fatalf("local mode should fall back, got %s", lenient.Mode())
	}
}
