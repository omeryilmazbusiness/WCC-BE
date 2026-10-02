package task

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

// Related types of rule tasks; auto-close matches on them.
const (
	RelatedConversation    = "conversation"
	RelatedVisaApplication = "visa_application"
	RelatedSupplierLink    = "supplier_link"
	RelatedBooking         = "booking"
)

// EnsureUnansweredTask asks the conversation owner to reply once the SLA is
// breached; one task per unanswered episode (keyed by its start).
func (s *Seeder) EnsureUnansweredTask(ctx context.Context, branchID, conversationID, assigneeID uuid.UUID, since time.Time) error {
	now := time.Now().UTC()
	due := now.Add(30 * time.Minute)
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: "Reply to unanswered conversation",
		Kind: domain.KindFollowUp, Priority: domain.PriorityCritical, Status: domain.StatusOpen,
		AssigneeID: assigneeID, RelatedType: RelatedConversation, RelatedID: conversationID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("conv:%s:unanswered:%d", conversationID, since.UTC().Unix()),
		SourceRule:     domain.RuleUnansweredMessage,
		CreatedAt:      now, UpdatedAt: now,
	})
}

// EnsureVisaFollowUpTask chases a visa application still undecided after the
// branch's follow-up window.
func (s *Seeder) EnsureVisaFollowUpTask(ctx context.Context, branchID, applicationID, assigneeID uuid.UUID, reference string) error {
	now := time.Now().UTC()
	due := now.Add(24 * time.Hour)
	title := "Follow up visa application"
	if ref := strings.TrimSpace(reference); ref != "" {
		title += " " + ref
	}
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: title,
		Kind: domain.KindFollowUp, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
		AssigneeID: assigneeID, RelatedType: RelatedVisaApplication, RelatedID: applicationID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("visa:%s:follow-up", applicationID),
		SourceRule:     domain.RuleVisaFollowUp,
		CreatedAt:      now, UpdatedAt: now,
	})
}

// EnsureSupplierConfirmTask asks operations to obtain a supplier confirmation.
func (s *Seeder) EnsureSupplierConfirmTask(ctx context.Context, branchID, linkID, assigneeID uuid.UUID, label string) error {
	now := time.Now().UTC()
	due := now.Add(24 * time.Hour)
	title := "Get supplier confirmation"
	if l := strings.TrimSpace(label); l != "" {
		title += ": " + l
	}
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: title,
		Kind: domain.KindCustom, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
		AssigneeID: assigneeID, RelatedType: RelatedSupplierLink, RelatedID: linkID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("supplier:%s:confirm", linkID),
		SourceRule:     domain.RuleSupplierConfirm,
		CreatedAt:      now, UpdatedAt: now,
	})
}

// EnsureMissingDocTask asks the booking owner to collect required documents
// that are still missing close to departure.
func (s *Seeder) EnsureMissingDocTask(ctx context.Context, branchID, bookingID, ownerID uuid.UUID, missing []string) error {
	now := time.Now().UTC()
	due := now.Add(24 * time.Hour)
	return s.ensureTask(ctx, domain.Task{
		ID: uuid.New(), BranchID: branchID, Title: "Collect missing documents: " + strings.Join(missing, ", "),
		Kind: domain.KindDocument, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
		AssigneeID: ownerID, RelatedType: RelatedBooking, RelatedID: bookingID, DueAt: &due,
		IdempotencyKey: fmt.Sprintf("booking:%s:missing-docs", bookingID),
		SourceRule:     domain.RuleMissingDocument,
		CreatedAt:      now, UpdatedAt: now,
	})
}

// CloseByRule completes every open task a rule created for one record, once
// the issue the rule tracked is resolved; returns how many were closed.
func (s *Seeder) CloseByRule(ctx context.Context, rule, relatedType string, relatedID uuid.UUID, outcome string) (int, error) {
	ctx = access.WithScope(ctx, access.System())
	closed := 0
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		items, err := s.tasks.ListOpenByRule(ctx, rule, relatedType, relatedID)
		if err != nil {
			return err
		}
		for i := range items {
			t := items[i]
			if err := t.CompleteWithOutcome(outcome); err != nil {
				return err
			}
			if err := s.tasks.Update(ctx, &t); err != nil {
				return err
			}
			closed++
		}
		return nil
	})
	return closed, err
}
