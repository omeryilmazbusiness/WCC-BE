package search

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Kind of searchable entity (T-233).
type Kind string

const (
	KindCustomer Kind = "customer"
	KindLead     Kind = "lead"
	KindBooking  Kind = "booking"
	KindPassport Kind = "passport"
)

// Kinds lists every searchable kind in display order.
var Kinds = []Kind{KindCustomer, KindLead, KindBooking, KindPassport}

// ParseKinds reads a kind filter ("customer,booking"); empty means all kinds.
// Unknown kinds are rejected so a typo never silently widens the search.
func ParseKinds(values ...string) ([]Kind, error) {
	var out []Kind
	seen := map[Kind]bool{}
	for _, v := range values {
		for _, part := range strings.Split(v, ",") {
			k := Kind(strings.ToLower(strings.TrimSpace(part)))
			if k == "" || seen[k] {
				continue
			}
			if !k.valid() {
				return nil, shared.NewValidation("unknown search kind: " + string(k))
			}
			seen[k] = true
			out = append(out, k)
		}
	}
	return out, nil
}

func (k Kind) valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

// Query is a normalized search request; empty Kinds searches every kind.
type Query struct {
	Text  string
	Kinds []Kind
	Limit int
}

type Hit struct {
	Kind     Kind      `json:"kind"`
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Subtitle string    `json:"subtitle"`
	HrefHint string    `json:"href_hint"`
	Score    int       `json:"score"`
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
	Search(ctx context.Context, branchID *uuid.UUID, q Query) ([]Hit, error)
}
