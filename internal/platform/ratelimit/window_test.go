package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemorySlidingWindow(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }

	for i := 1; i <= 3; i++ {
		if n, _ := m.Hit(ctx, "k", time.Minute); n != i {
			t.Fatalf("hit %d got %d", i, n)
		}
		now = now.Add(20 * time.Second)
	}
	// first hit (t=1000) is now exactly 60s old and falls out.
	if n, _ := m.Count(ctx, "k", time.Minute); n != 2 {
		t.Fatalf("count after slide got %d", n)
	}
	_ = m.Reset(ctx, "k")
	if n, _ := m.Count(ctx, "k", time.Minute); n != 0 {
		t.Fatalf("after reset got %d", n)
	}
}

type failing struct{}

func (failing) Hit(context.Context, string, time.Duration) (int, error) { return 0, errors.New("down") }
func (failing) Count(context.Context, string, time.Duration) (int, error) {
	return 0, errors.New("down")
}
func (failing) Reset(context.Context, string) error { return errors.New("down") }

func TestResilientFallsBack(t *testing.T) {
	ctx := context.Background()
	r := NewResilient(failing{}, NewMemory(), nil)
	for i := 1; i <= 2; i++ {
		n, err := r.Hit(ctx, "ip", time.Minute)
		if err != nil || n != i {
			t.Fatalf("hit %d: n=%d err=%v", i, n, err)
		}
	}
	if n, err := r.Count(ctx, "ip", time.Minute); err != nil || n != 2 {
		t.Fatalf("count n=%d err=%v", n, err)
	}
	if err := r.Reset(ctx, "ip"); err != nil {
		t.Fatal(err)
	}
}
