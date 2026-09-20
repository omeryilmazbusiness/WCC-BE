package shared

import (
	"context"
	"time"
)

// ObjectStore is the S3-compatible port (DIP).
// Adapters: MinIO/S3 stub or real SDK. Domain/app never import provider SDKs.
// Binary blobs live in object storage; only metadata is persisted in Postgres.
type ObjectStore interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (url string, err error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (url string, err error)
}
