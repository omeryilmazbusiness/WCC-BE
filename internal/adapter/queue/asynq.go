package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	QueueDefault  = "default"
	QueueCritical = "critical"
	QueueLow      = "low"
)

// Client implements shared.Enqueuer + shared.QueueInspector.
// Mode "memory" is used for unit tests / local without Redis.
// Mode "redis" uses Asynq with retry + archived (DLQ) queues.
type Client struct {
	log       *slog.Logger
	mode      string
	client    *asynq.Client
	inspector *asynq.Inspector
	rdb       redis.UniversalClient
	mu        sync.Mutex
	memJobs   map[string]shared.JobInfo
	memStats  shared.QueueStats
}

// AsynqClient is a backward-compatible alias used by the composition root.
type AsynqClient = Client

func NewClient(cfg config.RedisConfig, log *slog.Logger) (*Client, error) {
	if cfg.URL == "" || cfg.URL == "memory://" {
		log.Info("queue running in memory mode")
		return memoryClient(log), nil
	}

	opt, err := asynq.ParseRedisURI(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := asynq.NewClient(opt)
	inspector := asynq.NewInspector(opt)

	ropt, err := redis.ParseURL(cfg.URL)
	if err != nil {
		_ = client.Close()
		_ = inspector.Close()
		return nil, fmt.Errorf("parse redis url for ping: %w", err)
	}
	rdb := redis.NewClient(ropt)

	c := &Client{
		log:       log,
		mode:      "redis",
		client:    client,
		inspector: inspector,
		rdb:       rdb,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		log.Warn("redis not reachable; falling back to memory queue", "error", err)
		_ = c.Close()
		return memoryClient(log), nil
	}
	return c, nil
}

func memoryClient(log *slog.Logger) *Client {
	return &Client{
		log:      log,
		mode:     "memory",
		memJobs:  make(map[string]shared.JobInfo),
		memStats: shared.QueueStats{Mode: "memory"},
	}
}

// NewAsynqClient keeps backward-compatible constructor used by wire.go.
func NewAsynqClient(cfg config.RedisConfig, log *slog.Logger) *Client {
	c, err := NewClient(cfg, log)
	if err != nil {
		log.Error("queue init failed; falling back to memory", "error", err)
		return memoryClient(log)
	}
	return c
}

func (c *Client) Mode() string { return c.mode }

func (c *Client) Ping(ctx context.Context) error {
	if c.mode == "memory" {
		return nil
	}
	return c.rdb.Ping(ctx).Err()
}

func (c *Client) Close() error {
	if c.client != nil {
		_ = c.client.Close()
	}
	if c.inspector != nil {
		_ = c.inspector.Close()
	}
	if c.rdb != nil {
		_ = c.rdb.Close()
	}
	return nil
}

func (c *Client) Enqueue(ctx context.Context, name shared.JobName, payload []byte, opts shared.EnqueueOpts) (string, error) {
	queue := opts.Queue
	if queue == "" {
		queue = QueueDefault
	}
	maxRetry := opts.MaxRetry
	if maxRetry <= 0 {
		maxRetry = 5
	}

	if c.mode == "memory" {
		id := uuid.NewString()
		c.mu.Lock()
		c.memJobs[id] = shared.JobInfo{
			ID:       id,
			Type:     string(name),
			Queue:    queue,
			State:    "pending",
			MaxRetry: maxRetry,
		}
		c.memStats.Pending++
		c.mu.Unlock()
		c.log.Info("enqueued memory job", "id", id, "type", name, "queue", queue)
		return id, nil
	}

	task := asynq.NewTask(string(name), payload, asynq.Queue(queue), asynq.MaxRetry(maxRetry))
	asynqOpts := []asynq.Option{asynq.Queue(queue), asynq.MaxRetry(maxRetry)}
	if opts.ProcessIn > 0 {
		asynqOpts = append(asynqOpts, asynq.ProcessIn(opts.ProcessIn))
	}
	if opts.UniqueKey != "" {
		asynqOpts = append(asynqOpts, asynq.TaskID(opts.UniqueKey), asynq.Retention(24*time.Hour))
	}

	info, err := c.client.EnqueueContext(ctx, task, asynqOpts...)
	if err != nil {
		return "", fmt.Errorf("enqueue %s: %w", name, err)
	}
	c.log.Info("enqueued job", "id", info.ID, "type", name, "queue", info.Queue)
	return info.ID, nil
}

func (c *Client) Stats(ctx context.Context) (shared.QueueStats, error) {
	_ = ctx
	if c.mode == "memory" {
		c.mu.Lock()
		defer c.mu.Unlock()
		s := c.memStats
		s.Mode = "memory"
		return s, nil
	}

	var out shared.QueueStats
	out.Mode = "redis"
	for _, q := range []string{QueueCritical, QueueDefault, QueueLow} {
		qi, err := c.inspector.GetQueueInfo(q)
		if err != nil {
			// queue may not exist yet
			continue
		}
		out.Pending += qi.Pending
		out.Active += qi.Active
		out.Scheduled += qi.Scheduled
		out.Retry += qi.Retry
		out.Archived += qi.Archived
		out.Completed += qi.Completed
	}
	return out, nil
}

func (c *Client) GetJob(ctx context.Context, queue, jobID string) (shared.JobInfo, error) {
	_ = ctx
	if queue == "" {
		queue = QueueDefault
	}
	if c.mode == "memory" {
		c.mu.Lock()
		defer c.mu.Unlock()
		info, ok := c.memJobs[jobID]
		if !ok {
			return shared.JobInfo{}, shared.NewNotFound("job")
		}
		return info, nil
	}

	ti, err := c.inspector.GetTaskInfo(queue, jobID)
	if err != nil {
		return shared.JobInfo{}, shared.NewNotFound("job")
	}
	return shared.JobInfo{
		ID:       ti.ID,
		Type:     ti.Type,
		Queue:    ti.Queue,
		State:    ti.State.String(),
		Retried:  ti.Retried,
		MaxRetry: ti.MaxRetry,
		LastErr:  ti.LastErr,
		NextAt:   ti.NextProcessAt,
	}, nil
}

// EnqueueJSON is a helper for typed payloads.
func EnqueueJSON(ctx context.Context, e shared.Enqueuer, name shared.JobName, v any, opts shared.EnqueueOpts) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", shared.NewValidation("invalid job payload")
	}
	return e.Enqueue(ctx, name, b, opts)
}
