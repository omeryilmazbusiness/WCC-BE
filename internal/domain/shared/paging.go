package shared

import "math"

const (
	DefaultPageLimit = 25
	MaxPageLimit     = 100
)

// PageQuery is the canonical list contract (limit/offset + sort).
type PageQuery struct {
	Limit  int
	Offset int
	Sort   string // e.g. "created_at" or "-created_at"
}

// Normalize clamps limit/offset to safe bounds.
func (p PageQuery) Normalize() PageQuery {
	if p.Limit <= 0 {
		p.Limit = DefaultPageLimit
	}
	if p.Limit > MaxPageLimit {
		p.Limit = MaxPageLimit
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}

// PageMeta is returned in API envelope meta for list endpoints.
type PageMeta struct {
	Total      int64 `json:"total"`
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
	Page       int   `json:"page"`
	TotalPages int   `json:"total_pages"`
	Sort       string `json:"sort,omitempty"`
}

func NewPageMeta(total int64, q PageQuery) PageMeta {
	q = q.Normalize()
	page := 1
	if q.Limit > 0 {
		page = (q.Offset / q.Limit) + 1
	}
	totalPages := 0
	if q.Limit > 0 && total > 0 {
		totalPages = int(math.Ceil(float64(total) / float64(q.Limit)))
	}
	return PageMeta{
		Total:      total,
		Limit:      q.Limit,
		Offset:     q.Offset,
		Page:       page,
		TotalPages: totalPages,
		Sort:       q.Sort,
	}
}

// SortColumn returns the column name without direction prefix.
func (p PageQuery) SortColumn(fallback string) string {
	s := p.Sort
	if s == "" {
		return fallback
	}
	if s[0] == '-' || s[0] == '+' {
		s = s[1:]
	}
	if s == "" {
		return fallback
	}
	return s
}

// SortDesc reports whether sort is descending ("-field").
func (p PageQuery) SortDesc() bool {
	return len(p.Sort) > 0 && p.Sort[0] == '-'
}
