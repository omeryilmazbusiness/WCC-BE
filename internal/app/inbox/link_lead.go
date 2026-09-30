package inbox

import (
	"context"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// LeadLocator finds the branch of a lead visible to the caller; found is
// false when the lead does not exist or is out of scope.
type LeadLocator interface {
	LeadBranch(ctx context.Context, leadID uuid.UUID) (branchID uuid.UUID, found bool, err error)
}

func (s *Service) SetLeadLocator(l LeadLocator) { s.leadLocator = l }

type LinkLeadInput struct {
	ConversationID uuid.UUID
	LeadID         uuid.UUID
	ActorID        uuid.UUID
}

// LinkLead attaches a lead of the same branch to a conversation.
func (s *Service) LinkLead(ctx context.Context, in LinkLeadInput) (*domain.Conversation, error) {
	if in.LeadID == uuid.Nil {
		return nil, shared.NewValidation("lead_id is required")
	}
	if s.leadLocator == nil {
		return nil, shared.NewValidation("lead linking is not configured")
	}
	var out *domain.Conversation
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		c, err := s.repo.GetConversation(ctx, in.ConversationID)
		if err != nil {
			return err
		}
		if c == nil {
			return shared.NewNotFound("conversation")
		}
		branchID, found, err := s.leadLocator.LeadBranch(ctx, in.LeadID)
		if err != nil {
			return err
		}
		if !found {
			return shared.NewNotFound("lead")
		}
		if branchID != c.BranchID {
			return shared.NewValidation("lead belongs to another branch")
		}
		leadID := in.LeadID
		c.LeadID = &leadID
		c.UpdatedAt = s.now()
		if err := s.repo.UpdateConversation(ctx, c); err != nil {
			return err
		}
		out = c
		return nil
	})
	return out, err
}
