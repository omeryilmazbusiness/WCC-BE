package request

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Page parses canonical list query params:
//   ?limit=&offset=&page=&sort=
// Prefer limit/offset; page is converted when offset is absent.
func Page(r *http.Request) shared.PageQuery {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	page, _ := strconv.Atoi(q.Get("page"))
	sort := strings.TrimSpace(q.Get("sort"))

	pq := shared.PageQuery{Limit: limit, Offset: offset, Sort: sort}.Normalize()
	if q.Get("offset") == "" && page > 1 {
		pq.Offset = (page - 1) * pq.Limit
	}
	return pq
}

// FilterString returns trimmed query value or empty.
func FilterString(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}
