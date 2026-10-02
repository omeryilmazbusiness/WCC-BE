package task

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	paymentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/payment"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

// EnsurePromiseFollowUp creates the follow-up task for a payment promise (T-274),
// due on the promised date, and returns its id (existing or new).
func (s *Seeder) EnsurePromiseFollowUp(ctx context.Context, p *paymentdomain.Promise, assigneeID uuid.UUID, dueAt time.Time) (uuid.UUID, error) {
	now := time.Now().UTC()
	return s.ensureTaskID(ctx, domain.Task{
		ID: uuid.New(), BranchID: p.BranchID,
		Title: fmt.Sprintf("Payment promised %d %s", p.Amount, p.Currency),
		Kind:  domain.KindPayment, Priority: domain.PriorityMinor, Status: domain.StatusOpen,
		AssigneeID: assigneeID, RelatedType: "booking", RelatedID: p.BookingID, DueAt: &dueAt,
		IdempotencyKey: "payment-promise:" + p.ID.String() + ":follow-up",
		SourceRule:     domain.RulePaymentPromise,
		CreatedAt:      now, UpdatedAt: now,
	})
}

// EnsurePromiseBroken creates the urgent chase task once a promise is broken.
func (s *Seeder) EnsurePromiseBroken(ctx context.Context, p *paymentdomain.Promise, assigneeID uuid.UUID) error {
	now := time.Now().UTC()
	due := now.Add(24 * time.Hour)
	_, err := s.ensureTaskID(ctx, domain.Task{
		ID: uuid.New(), BranchID: p.BranchID,
		Title: fmt.Sprintf("Broken payment promise %d %s (%s)", p.Amount, p.Currency, p.PromisedOn.Format(time.DateOnly)),
		Kind:  domain.KindPayment, Priority: domain.PriorityMajor, Status: domain.StatusOpen,
		AssigneeID: assigneeID, RelatedType: "booking", RelatedID: p.BookingID, DueAt: &due,
		IdempotencyKey: "payment-promise:" + p.ID.String() + ":broken",
		SourceRule:     domain.RulePaymentPromiseBroken,
		CreatedAt:      now, UpdatedAt: now,
	})
	return err
}

// CloseTask completes (done) or cancels an open task; closed tasks are left alone.
func (s *Seeder) CloseTask(ctx context.Context, taskID uuid.UUID, done bool, outcome string) error {
	ctx = access.WithScope(ctx, access.System())
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		t, err := s.tasks.FindByID(ctx, taskID)
		if err != nil {
			return err
		}
		if t.Status == domain.StatusDone || t.Status == domain.StatusCancelled {
			return nil
		}
		if done {
			err = t.CompleteWithOutcome(outcome)
		} else {
			err = t.TransitionTo(domain.StatusCancelled)
			t.Outcome = outcome
		}
		if err != nil {
			return err
		}
		return s.tasks.Update(ctx, t)
	})
}

func (s *Seeder) ensureTaskID(ctx context.Context, t domain.Task) (uuid.UUID, error) {
	ctx = access.WithScope(ctx, access.System())
	id := t.ID
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if existing, err := s.tasks.FindByIdempotencyKey(ctx, t.IdempotencyKey); err == nil && existing != nil {
			id = existing.ID
			return nil
		}
		return s.tasks.Create(ctx, &t)
	})
	return id, err
}
