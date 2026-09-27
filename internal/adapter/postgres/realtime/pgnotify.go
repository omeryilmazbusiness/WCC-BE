// Package realtime carries realtime signals between processes over Postgres
// LISTEN/NOTIFY, so any API replica can serve any user's stream.
package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	apprealtime "github.com/wodi-crm/wodi-crm-be/internal/app/realtime"
)

// Channel is shared with the notifications trigger (migration 00029).
const Channel = "wcc_events"

// Notifier implements realtime.Announcer with pg_notify.
type Notifier struct {
	pool *pgxpool.Pool
}

func NewNotifier(pool *pgxpool.Pool) *Notifier { return &Notifier{pool: pool} }

func (n *Notifier) Announce(ctx context.Context, s apprealtime.Signal) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = n.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, string(raw))
	return err
}

// Broadcaster receives decoded signals (the in-process hub).
type Broadcaster interface {
	Broadcast(s apprealtime.Signal)
}

// Listen holds one pooled connection on LISTEN and forwards signals until ctx
// ends, reconnecting with backoff when the connection drops.
func Listen(ctx context.Context, pool *pgxpool.Pool, hub Broadcaster, log *slog.Logger) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := listenOnce(ctx, pool, hub, log)
		if ctx.Err() != nil {
			return
		}
		log.Warn("realtime listener disconnected", "error", err, "retry_in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, hub Broadcaster, log *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	defer func() {
		// The connection returns to the pool; it must not keep listening.
		unlisten, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlisten, "UNLISTEN "+Channel)
	}()
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var s apprealtime.Signal
		if err := json.Unmarshal([]byte(n.Payload), &s); err != nil || s.Type == "" {
			log.Warn("realtime: dropped malformed signal", "error", err)
			continue
		}
		hub.Broadcast(s)
	}
}
