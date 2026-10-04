package lead

import (
	"strings"
	"time"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// profileRequest carries next_follow_up_at as RFC 3339.
type profileRequest struct {
	Email          string  `json:"email"`
	Segment        string  `json:"segment"`
	CompanyName    string  `json:"company_name"`
	TaxNumber      string  `json:"tax_number"`
	TaxOffice      string  `json:"tax_office"`
	Priority       string  `json:"priority"`
	Intent         string  `json:"intent"`
	NextFollowUpAt *string `json:"next_follow_up_at"`
}

func (r *profileRequest) toDomain() (domain.Profile, error) {
	if r == nil {
		return domain.Profile{}, nil
	}
	out := domain.Profile{
		Email: r.Email, Segment: domain.Segment(r.Segment), CompanyName: r.CompanyName,
		TaxNumber: r.TaxNumber, TaxOffice: r.TaxOffice,
		Priority: domain.Priority(r.Priority), Intent: domain.Intent(r.Intent),
	}
	if r.NextFollowUpAt != nil && strings.TrimSpace(*r.NextFollowUpAt) != "" {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(*r.NextFollowUpAt))
		if err != nil {
			verr := shared.NewValidation("invalid lead profile")
			verr.Details = map[string]any{"next_follow_up_at": "must be RFC 3339"}
			return out, verr
		}
		out.NextFollowUpAt = &at
	}
	return out, nil
}

func mapProfile(p domain.Profile) map[string]any {
	var followUp any
	if p.NextFollowUpAt != nil {
		followUp = p.NextFollowUpAt.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"email":             p.Email,
		"segment":           p.Segment,
		"company_name":      p.CompanyName,
		"tax_number":        p.TaxNumber,
		"tax_office":        p.TaxOffice,
		"priority":          p.Priority,
		"intent":            p.Intent,
		"next_follow_up_at": followUp,
	}
}
