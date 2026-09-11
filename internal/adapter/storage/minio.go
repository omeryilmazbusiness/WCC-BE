package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// MinIO is an S3-compatible adapter behind ObjectStore.
// Local/dev returns deterministic stub URLs (no network). Swap the body for
// minio-go / AWS SDK without changing app ports (OCP / DIP).
type MinIO struct {
	cfg config.StorageConfig
}

func NewMinIO(cfg config.StorageConfig) *MinIO {
	return &MinIO{cfg: cfg}
}

func (m *MinIO) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	if strings.TrimSpace(contentType) == "" {
		return "", shared.NewValidation("content_type is required")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return m.buildURL(key, map[string]string{
		"upload":       "1",
		"content-type": contentType,
		"expires":      fmt.Sprintf("%d", time.Now().UTC().Add(ttl).Unix()),
	}), nil
}

func (m *MinIO) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return m.buildURL(key, map[string]string{
		"expires": fmt.Sprintf("%d", time.Now().UTC().Add(ttl).Unix()),
	}), nil
}

func (m *MinIO) buildURL(key string, query map[string]string) string {
	base := strings.TrimRight(m.cfg.PublicURL, "/")
	bucket := strings.Trim(m.cfg.Bucket, "/")
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s", base, bucket, key))
	if err != nil {
		return fmt.Sprintf("%s/%s/%s", base, bucket, key)
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func validateKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return shared.NewValidation("storage key is required")
	}
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") {
		return shared.NewValidation("invalid storage key")
	}
	return nil
}
