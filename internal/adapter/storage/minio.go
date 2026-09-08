package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

// MinIO is an S3-compatible stub that returns deterministic local URLs.
// Wire a real AWS SDK / minio-go client in P0+ without changing the port.
type MinIO struct {
	cfg config.StorageConfig
}

func NewMinIO(cfg config.StorageConfig) *MinIO {
	return &MinIO{cfg: cfg}
}

func (m *MinIO) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	_ = contentType
	_ = ttl
	return fmt.Sprintf("%s/%s/%s?upload=1", m.cfg.PublicURL, m.cfg.Bucket, key), nil
}

func (m *MinIO) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	_ = ttl
	return fmt.Sprintf("%s/%s/%s", m.cfg.PublicURL, m.cfg.Bucket, key), nil
}
