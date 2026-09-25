package search

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// Kind of searchable entity (T-233).
type Kind string

const (
	KindCustomer Kind = "customer"
	KindLead     Kind = "lead"
	KindBooking  Kind = "booking"
	KindPassport Kind = "passport"
)

type Hit struct {
	Kind      Kind      `json:"kind"`
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle"`
	HrefHint  string    `json:"href_hint"`
	Score     int       `json:"score"`
}

// NormalizeQuery trims and lowercases; returns empty if too short.
func NormalizeQuery(q string) string {
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return ""
	}
	return q
}

// Searcher is the cross-entity search port (DIP).
type Searcher interface {
	Search(ctx context.Context, branchID uuid.UUID, q string, limit int) ([]Hit, error)
}
