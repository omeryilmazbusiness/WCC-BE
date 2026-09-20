package request_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/request"
)

func TestPageFromQuery(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x?limit=10&page=3&sort=-created_at", nil)
	p := request.Page(r)
	if p.Limit != 10 || p.Offset != 20 || p.Sort != "-created_at" {
		t.Fatalf("got %+v", p)
	}
}
