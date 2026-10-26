package shared

import (
	"context"
	"time"
)

// ObjectStore is the leftover document-presign port (DIP).
// Company logos live in Postgres; there is no S3/MinIO.
type ObjectStore interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, err error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, err error)
}
