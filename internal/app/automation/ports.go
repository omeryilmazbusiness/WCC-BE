// Package automation turns domain events and scheduled sweeps into
// notifications and rule tasks (Epic 22). It owns no data: every effect goes
// through the owning module's service.
package automation

import (
	"context"
	"time"

	"github.com/google/uuid"

	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	documentdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/document"
	notificationdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	taskdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/task"
)

// Branch is an active branch and the zone its local schedules run in.
type Branch struct {
	ID       uuid.UUID
	TimeZone string
}

// Location falls back to UTC for unknown zones so one bad row cannot stop a sweep.
func (b Branch) Location() *time.Location {
	if loc, err := time.LoadLocation(b.TimeZone); err == nil {
		return loc
	}
	return time.UTC
}

type BookingRef struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	OwnerID       uuid.UUID
	DepartureCode string
	DepartDate    time.Time
}

type LeadRef struct {
	ID        uuid.UUID
	BranchID  uuid.UUID
	OwnerID   uuid.UUID
	FullName  string
	IdleSince time.Time
}

type PassportRef struct {
	CustomerID uuid.UUID
	BranchID   uuid.UUID
	FullName   string
	ExpiresAt  time.Time
	OwnerID    uuid.UUID
	BookingID  uuid.UUID
}

// OverdueScheduleRef is a payment schedule past its due date plus the branch
// payment_overdue_hours grace.
type OverdueScheduleRef struct {
	ScheduleID uuid.UUID
	BookingID  uuid.UUID
	BranchID   uuid.UUID
	OwnerID    uuid.UUID
	DueAt      time.Time
	Amount     int64
	Currency   string
}

// Queries are the cross-module candidate lists the sweeps work from.
type Queries interface {
	Branches(ctx context.Context) ([]Branch, error)
	BookingsAwaitingDocs(ctx context.Context, now time.Time, limit int) ([]BookingRef, error)
	IdleLeads(ctx context.Context, now time.Time, limit int) ([]LeadRef, error)
	ExpiringPassports(ctx context.Context, today, horizon time.Time, limit int) ([]PassportRef, error)
	OverdueSchedules(ctx context.Context, now time.Time, limit int) ([]OverdueScheduleRef, error)
}

// Notifier delivers in-app notifications.
type Notifier interface {
	Emit(ctx context.Context, in appnotification.EmitInput) (*notificationdomain.Notification, error)
	EmitToRoles(ctx context.Context, branchID uuid.UUID, roles []string, in appnotification.EmitInput) (int, error)
}

// Staff lists active branch users by role (task assignees for role-owned work).
type Staff interface {
	ListActiveByRoles(ctx context.Context, branchID uuid.UUID, roles []string) ([]appnotification.UserRef, error)
}

// RuleTasks opens and closes idempotent rule tasks.
type RuleTasks interface {
	EnsureUnansweredTask(ctx context.Context, branchID, conversationID, assigneeID uuid.UUID, since time.Time) error
	EnsureVisaFollowUpTask(ctx context.Context, branchID, applicationID, assigneeID uuid.UUID, reference string) error
	EnsureSupplierConfirmTask(ctx context.Context, branchID, linkID, assigneeID uuid.UUID, label string) error
	EnsureMissingDocTask(ctx context.Context, branchID, bookingID, ownerID uuid.UUID, missing []string) error
	EnsureTargetRecoveryTask(ctx context.Context, branchID, targetID, assigneeID uuid.UUID, label string, deficit int64) error
	CloseByRule(ctx context.Context, rule, relatedType string, relatedID uuid.UUID, outcome string) (int, error)
}

// TaskReader loads a task for explicit reminders.
type TaskReader interface {
	FindByID(ctx context.Context, id uuid.UUID) (*taskdomain.Task, error)
}

// BookingRecomputer re-derives a booking's lifecycle status.
type BookingRecomputer interface {
	RecomputeBooking(ctx context.Context, bookingID uuid.UUID) error
}

// TargetRecomputer refreshes a branch's revenue target snapshots.
type TargetRecomputer interface {
	RecomputeTargets(ctx context.Context, branchID uuid.UUID) error
}

// Checklists reads a booking's document checklist.
type Checklists interface {
	Checklist(ctx context.Context, bookingID uuid.UUID) (*documentdomain.Checklist, error)
}

var (
	managerRoles     = []string{"manager", "gm"}
	operationsRoles  = []string{"operations", "manager"}
	integrationRoles = []string{"gm", "manager", "admin"}
)
