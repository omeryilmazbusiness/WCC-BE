package lead

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// PackageChecker tells whether a catalogue package belongs to a branch.
type PackageChecker interface {
	PackageInBranch(ctx context.Context, packageID, branchID uuid.UUID) (bool, error)
}

func (s *Service) SetPackageChecker(p PackageChecker) { s.packages = p }

// UpdateDetailsInput edits a lead's contact, profile and trip interest. Nil
// fields are left unchanged; a non-nil Profile or Interest replaces it whole.
type UpdateDetailsInput struct {
	LeadID    uuid.UUID
	FullName  *string
	Phone     *string
	Notes     *string
	Profile   *domain.Profile
	Interest  *domain.TripInterest
	ActorID   uuid.UUID
	IP        string
	UserAgent string
}

func (s *Service) UpdateDetails(ctx context.Context, in UpdateDetailsInput) (*domain.Lead, error) {
	var out *domain.Lead
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		l, err := s.repo.FindByID(ctx, in.LeadID)
		if err != nil {
			return shared.NewNotFound("lead")
		}
		before := *l
		if in.FullName != nil {
			name := strings.TrimSpace(*in.FullName)
			if name == "" {
				return shared.NewValidation("full_name is required")
			}
			l.FullName = name
		}
		if in.Phone != nil {
			phone := shared.NormalizePhone(*in.Phone)
			if phone == "" {
				return shared.NewValidation("phone is required")
			}
			l.Phone = phone
		}
		if in.Notes != nil {
			l.Notes = *in.Notes
		}
		if in.Interest != nil {
			interest, err := s.checkInterest(ctx, *in.Interest, l.BranchID)
			if err != nil {
				return err
			}
			l.Interest = interest
		}
		if in.Profile != nil {
			profile, err := in.Profile.Normalize(time.Now().UTC(), l.Interest.TravelDate, l.Profile.NextFollowUpAt)
			if err != nil {
				return err
			}
			l.Profile = profile
		}
		l.UpdatedAt = time.Now().UTC()
		if err := s.repo.Update(ctx, l); err != nil {
			return err
		}
		out = l
		return s.recordAudit(ctx, in.ActorID, "lead.updated", l.ID, &l.BranchID, before, l, in.IP, in.UserAgent)
	})
	return out, err
}

func (s *Service) checkInterest(ctx context.Context, t domain.TripInterest, branchID uuid.UUID) (domain.TripInterest, error) {
	t, err := t.Normalize(time.Now().UTC())
	if err != nil {
		return t, err
	}
	if t.PackageID == nil || *t.PackageID == uuid.Nil {
		t.PackageID = nil
		return t, nil
	}
	if s.packages == nil {
		return t, shared.NewValidation("package selection is not available")
	}
	ok, err := s.packages.PackageInBranch(ctx, *t.PackageID, branchID)
	if err != nil {
		return t, err
	}
	if !ok {
		verr := shared.NewValidation("invalid trip interest")
		verr.Details = map[string]any{"package_id": "not a package of this branch"}
		return t, verr
	}
	return t, nil
}
