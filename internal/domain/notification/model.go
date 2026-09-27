package notification

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Severity levels for in-app alerts (mandatory channel).
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func ValidSeverity(s Severity) bool {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}

// Status lifecycle: open → acknowledged → resolved (or open → resolved).
type Status string

const (
	StatusOpen         Status = "open"
	StatusAcknowledged Status = "acknowledged"
	StatusResolved     Status = "resolved"
)

func ValidStatus(s Status) bool {
	switch s {
	case StatusOpen, StatusAcknowledged, StatusResolved:
		return true
	default:
		return false
	}
}

// Canonical kinds used by the escalation matrix (T-174).
const (
	KindMessageSLA          = "message.sla_breached"
	KindLeadNoFollowUp      = "lead.no_follow_up"
	KindTaskOverdue         = "task.overdue"
	KindTaskEscalated       = "task.escalated"
	KindPaymentOverdue      = "payment.overdue"
	KindDocumentMissing     = "document.missing"
	KindDocumentExpiring    = "document.expiring"
	KindTargetBehind        = "target.behind"
	KindIntegrationDown     = "integration.unhealthy"
	KindBookingConfirmed    = "booking.confirmed"
	KindSupplierUnconfirmed = "supplier.unconfirmed"
	KindMessageSLAWarning   = "message.sla_warning"
	KindPaymentDue          = "payment.due"
	KindPassportExpiring    = "passport.expiring"
	KindVisaFollowUp        = "visa.follow_up"
	KindReportReady         = "report.ready"
	KindImportCompleted     = "import.completed"
	KindAISummary           = "ai.summary"
	KindTaskReminder        = "task.reminder"
)

// metaEscalatedFrom marks a notification produced by escalation; escalated
// copies never escalate again.
const metaEscalatedFrom = "escalated_from"

// EscalationMeta is the meta attached to an escalated copy.
func EscalationMeta(source Notification) map[string]any {
	return map[string]any{metaEscalatedFrom: source.ID.String(), "source_recipient": source.RecipientUserID.String()}
}

// IsEscalation reports whether the notification is an escalated copy.
func (n Notification) IsEscalation() bool {
	if len(n.MetaJSON) == 0 {
		return false
	}
	var meta map[string]any
	if err := json.Unmarshal(n.MetaJSON, &meta); err != nil {
		return false
	}
	_, ok := meta[metaEscalatedFrom]
	return ok
}

// Notification is the durable in-app alert for one recipient.
type Notification struct {
	ID              uuid.UUID
	BranchID        uuid.UUID
	RecipientUserID uuid.UUID
	Kind            string
	Severity        Severity
	Title           string
	Body            string
	EntityType      string
	EntityID        *uuid.UUID
	GroupKey        string
	OccurrenceCount int
	Status          Status
	HrefHint        string
	MetaJSON        json.RawMessage
	CreatedAt       time.Time
	UpdatedAt       time.Time
	AcknowledgedAt  *time.Time
	AcknowledgedBy  *uuid.UUID
	ResolvedAt      *time.Time
	ResolvedBy      *uuid.UUID
}

// Preference controls optional external channels; in-app is always on (T-176).
type Preference struct {
	UserID       uuid.UUID
	EmailEnabled bool
	PushEnabled  bool
	UpdatedAt    time.Time
}

func DefaultPreference(userID uuid.UUID) Preference {
	return Preference{
		UserID:       userID,
		EmailEnabled: false,
		PushEnabled:  false,
		UpdatedAt:    time.Now().UTC(),
	}
}

// Acknowledge marks the alert as seen/owned without closing it.
func (n *Notification) Acknowledge(actorID uuid.UUID) error {
	if n.Status == StatusResolved {
		return shared.NewInvalidState("resolved notification cannot be acknowledged")
	}
	if n.Status == StatusAcknowledged {
		return nil
	}
	now := time.Now().UTC()
	n.Status = StatusAcknowledged
	n.AcknowledgedAt = &now
	aid := actorID
	n.AcknowledgedBy = &aid
	n.UpdatedAt = now
	return nil
}

// Resolve closes the alert (terminal).
func (n *Notification) Resolve(actorID uuid.UUID) error {
	if n.Status == StatusResolved {
		return nil
	}
	now := time.Now().UTC()
	n.Status = StatusResolved
	n.ResolvedAt = &now
	rid := actorID
	n.ResolvedBy = &rid
	n.UpdatedAt = now
	return nil
}

// BumpOccurrence updates a grouped open alert (high-volume grouping, T-175).
func (n *Notification) BumpOccurrence(title, body string, severity Severity) {
	n.OccurrenceCount++
	if title != "" {
		n.Title = title
	}
	if body != "" {
		n.Body = body
	}
	if ValidSeverity(severity) && severityRank(severity) >= severityRank(n.Severity) {
		n.Severity = severity
	}
	n.UpdatedAt = time.Now().UTC()
}

func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	default:
		return 1
	}
}

// BuildGroupKey produces a stable key for high-volume merging.
func BuildGroupKey(kind string, branchID uuid.UUID, entityType string, entityID *uuid.UUID) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ""
	}
	rule := MatchRule(kind)
	if rule != nil && rule.Groupable {
		return kind + ":branch:" + branchID.String()
	}
	if entityID != nil {
		et := entityType
		if et == "" {
			et = "entity"
		}
		return kind + ":" + et + ":" + entityID.String()
	}
	return kind + ":branch:" + branchID.String() + ":" + uuid.New().String()
}

// DisplayTitle returns the analyst-facing title, including grouped counts.
func DisplayTitle(n Notification) string {
	if n.OccurrenceCount <= 1 {
		return n.Title
	}
	return GroupLabel(n.Kind, n.OccurrenceCount, n.Title)
}

// GroupLabel builds the analyst-facing label for high-volume items.
func GroupLabel(kind string, count int, baseTitle string) string {
	c := strconv.Itoa(count)
	switch kind {
	case KindMessageSLA:
		return c + " conversations overdue"
	case KindTaskOverdue, KindTaskEscalated:
		return c + " tasks need attention"
	case KindPaymentOverdue:
		return c + " payments overdue"
	case KindDocumentMissing, KindDocumentExpiring:
		return c + " document alerts"
	case KindLeadNoFollowUp:
		return c + " leads without follow-up"
	case KindTargetBehind:
		return c + " targets behind pace"
	case KindIntegrationDown:
		return c + " integration issues"
	case KindSupplierUnconfirmed:
		return c + " unconfirmed suppliers"
	case KindMessageSLAWarning:
		return c + " conversations near SLA"
	case KindPaymentDue:
		return c + " payments due"
	case KindPassportExpiring:
		return c + " passports expiring"
	case KindVisaFollowUp:
		return c + " visas awaiting decision"
	default:
		return c + " alerts — " + baseTitle
	}
}

// ListFilter for recipient inbox.
type ListFilter struct {
	RecipientUserID uuid.UUID
	Status          Status // empty = open+acknowledged (active)
	IncludeResolved bool
	Kinds           []string
	Limit           int
	Offset          int
}

// KindSummary counts a recipient's active notifications of one kind.
type KindSummary struct {
	Kind         string
	Severity     Severity
	Open         int
	Acknowledged int
	Occurrences  int
	LatestAt     time.Time
	Title        string
	Href         string
}

// Repository is the persistence port (DIP).
type Repository interface {
	Create(ctx context.Context, n *Notification) error
	Update(ctx context.Context, n *Notification) error
	FindByID(ctx context.Context, id uuid.UUID) (*Notification, error)
	FindOpenByGroup(ctx context.Context, recipientUserID uuid.UUID, groupKey string) (*Notification, error)
	List(ctx context.Context, f ListFilter) ([]Notification, int, error)
	CountUnread(ctx context.Context, recipientUserID uuid.UUID) (int, error)
	// SummarizeActive groups open and acknowledged notifications by kind,
	// most urgent first.
	SummarizeActive(ctx context.Context, recipientUserID uuid.UUID) ([]KindSummary, error)
	ListOpenOlderThan(ctx context.Context, olderThan time.Time, limit int) ([]Notification, error)
	// ListEscalationCandidates returns open, non-escalated notifications of
	// the given kinds created before olderThan, oldest first.
	ListEscalationCandidates(ctx context.Context, kinds []string, olderThan time.Time, limit int) ([]Notification, error)

	GetPreference(ctx context.Context, userID uuid.UUID) (*Preference, error)
	UpsertPreference(ctx context.Context, p *Preference) error
}
