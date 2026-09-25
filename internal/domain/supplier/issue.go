package supplier

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// IssueKind for supplier performance history (T-235).
type IssueKind string

const (
	IssueNote         IssueKind = "note"
	IssueDelay        IssueKind = "delay"
	IssueQuality      IssueKind = "quality"
	IssueCancellation IssueKind = "cancellation"
	IssueInvoice      IssueKind = "invoice"
	IssueOther        IssueKind = "other"
)

type IssueSeverity string

const (
	IssueInfo     IssueSeverity = "info"
	IssueWarning  IssueSeverity = "warning"
	IssueCritical IssueSeverity = "critical"
)

// IssueEvent is an append-only supplier history row.
type IssueEvent struct {
	ID         uuid.UUID
	BranchID   uuid.UUID
	SupplierID uuid.UUID
	LinkID     *uuid.UUID
	Kind       IssueKind
	Severity   IssueSeverity
	Note       string
	ActorID    *uuid.UUID
	CreatedAt  time.Time
}

func (e *IssueEvent) Normalize() error {
	e.Note = strings.TrimSpace(e.Note)
	if e.SupplierID == uuid.Nil {
		return shared.NewValidation("supplier_id is required")
	}
	if e.Note == "" {
		return shared.NewValidation("note is required")
	}
	switch e.Kind {
	case IssueNote, IssueDelay, IssueQuality, IssueCancellation, IssueInvoice, IssueOther:
	default:
		e.Kind = IssueNote
	}
	switch e.Severity {
	case IssueInfo, IssueWarning, IssueCritical:
	default:
		e.Severity = IssueInfo
	}
	return nil
}
