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
	c := queue.NewAsynqClient(config.RedisConfig{URL: "memory://"}, log)

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
