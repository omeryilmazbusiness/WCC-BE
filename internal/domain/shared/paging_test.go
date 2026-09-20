package shared

import "testing"

func TestPageQueryNormalize(t *testing.T) {
	q := PageQuery{Limit: 0, Offset: -5}.Normalize()
	if q.Limit != DefaultPageLimit || q.Offset != 0 {
		t.Fatalf("got %+v", q)
	}
	q = PageQuery{Limit: 500}.Normalize()
	if q.Limit != MaxPageLimit {
		t.Fatalf("limit=%d", q.Limit)
	}
}

func TestNewPageMeta(t *testing.T) {
	meta := NewPageMeta(100, PageQuery{Limit: 25, Offset: 50, Sort: "-created_at"})
	if meta.Page != 3 || meta.TotalPages != 4 || meta.Sort != "-created_at" {
		t.Fatalf("meta=%+v", meta)
	}
}

func TestSortHelpers(t *testing.T) {
	q := PageQuery{Sort: "-created_at"}
	if q.SortColumn("id") != "created_at" || !q.SortDesc() {
		t.Fatal("desc sort parse failed")
	}
	q = PageQuery{Sort: ""}
	if q.SortColumn("id") != "id" || q.SortDesc() {
		t.Fatal("fallback sort failed")
	}
}
