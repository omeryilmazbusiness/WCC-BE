package ratelimit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis is a sorted-set sliding window shared across instances.
type Redis struct {
	rdb    redis.UniversalClient
	prefix string
}

func NewRedis(rdb redis.UniversalClient, prefix string) *Redis {
	return &Redis{rdb: rdb, prefix: prefix}
}

func (r *Redis) Hit(ctx context.Context, key string, window time.Duration) (int, error) {
	k := r.prefix + key
	now := time.Now()
	member := strconv.FormatInt(now.UnixNano(), 10) + "-" + nonce()
	var card *redis.IntCmd
	_, err := r.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.ZRemRangeByScore(ctx, k, "-inf", strconv.FormatInt(now.Add(-window).UnixMilli(), 10))
		p.ZAdd(ctx, k, redis.Z{Score: float64(now.UnixMilli()), Member: member})
		card = p.ZCard(ctx, k)
		p.PExpire(ctx, k, window)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(card.Val()), nil
}

func (r *Redis) Count(ctx context.Context, key string, window time.Duration) (int, error) {
	k := r.prefix + key
	min := "(" + strconv.FormatInt(time.Now().Add(-window).UnixMilli(), 10)
	n, err := r.rdb.ZCount(ctx, k, min, "+inf").Result()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (r *Redis) Reset(ctx context.Context, key string) error {
	return r.rdb.Del(ctx, r.prefix+key).Err()
}

func nonce() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
