package storage

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Local is an in-process ObjectStore for leftover document-presign routes.
// Company logos live in Postgres (company_logos); there is no S3/MinIO.
type Local struct{}

func New() *Local {
	return &Local{}
}

func (l *Local) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	if strings.TrimSpace(contentType) == "" {
		return "", shared.NewValidation("content_type is required")
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return buildURL(key, map[string]string{
		"upload":       "1",
		"content-type": contentType,
		"expires":      fmt.Sprintf("%d", time.Now().UTC().Add(ttl).Unix()),
	}), nil
}

func (l *Local) PresignGet(_ context.Context, key string, ttl time.Duration) (string, error) {
	if err := validateKey(key); err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return buildURL(key, map[string]string{
		"expires": fmt.Sprintf("%d", time.Now().UTC().Add(ttl).Unix()),
	}), nil
}

func buildURL(key string, query map[string]string) string {
	u, err := url.Parse("/files/" + key)
	if err != nil {
		return "/files/" + key
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
