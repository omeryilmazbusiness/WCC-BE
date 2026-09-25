package shared

import "testing"

func TestPageQueryNormalizeGuarantees(t *testing.T) {
	// T-212: pagination clamps are part of DoD — list endpoints must never unbounded-scan.
	q := PageQuery{Limit: 0, Offset: -5}.Normalize()
	if q.Limit != DefaultPageLimit {
		t.Fatalf("default limit got %d", q.Limit)
	}
	if q.Offset != 0 {
		t.Fatalf("offset floor got %d", q.Offset)
	}
	q = PageQuery{Limit: 10_000}.Normalize()
	if q.Limit != MaxPageLimit {
		t.Fatalf("max clamp got %d want %d", q.Limit, MaxPageLimit)
	}
	meta := NewPageMeta(250, PageQuery{Limit: 25, Offset: 50})
	if meta.Page != 3 || meta.TotalPages != 10 {
		t.Fatalf("meta %#v", meta)
	}
	meta = NewPageMeta(0, PageQuery{Limit: 25})
	if meta.TotalPages != 0 || meta.Page != 1 {
		t.Fatalf("empty meta %#v", meta)
	}
}
