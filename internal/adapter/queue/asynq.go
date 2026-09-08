package queue

import (
	"log/slog"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

// AsynqClient is a placeholder for Redis+Asynq wiring (reminders, webhook retry, import).
// Real client is created in cmd/worker; API process only enqueues via this port later.
type AsynqClient struct {
	log *slog.Logger
	url string
}

func NewAsynqClient(cfg config.RedisConfig, log *slog.Logger) *AsynqClient {
	return &AsynqClient{log: log, url: cfg.URL}
}

func (c *AsynqClient) Ping() error {
	c.log.Debug("asynq client stub ready", "redis_url", c.url)
	return nil
}

func (c *AsynqClient) Close() error { return nil }
