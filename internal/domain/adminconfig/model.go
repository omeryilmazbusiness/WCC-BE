package adminconfig

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// SLAPolicy is admin-editable first-response SLA (T-220).
type SLAPolicy struct {
	ID                   uuid.UUID `json:"id"`
	BranchID             uuid.UUID `json:"branch_id"`
	Channel              string    `json:"channel"`
	FirstResponseSeconds int       `json:"first_response_seconds"`
}

func (p *SLAPolicy) Normalize() error {
	p.Channel = strings.TrimSpace(p.Channel)
	if p.Channel == "" {
		p.Channel = "*"
	}
	if p.FirstResponseSeconds <= 0 {
		return shared.NewValidation("first_response_seconds must be > 0")
	}
	return nil
}

// EscalationOverride overlays DefaultRules (T-221).
type EscalationOverride struct {
	BranchID             uuid.UUID `json:"branch_id"`
	Kind                 string    `json:"kind"`
	EscalateAfterSeconds int       `json:"escalate_after_seconds"`
	EscalateToRoles      []string  `json:"escalate_to_roles"`
	Enabled              bool      `json:"enabled"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// LostReason is a branch taxonomy row (T-223).
type LostReason struct {
	ID           uuid.UUID `json:"id"`
	BranchID     uuid.UUID `json:"branch_id"`
	Code         string    `json:"code"`
	LabelEn      string    `json:"label_en"`
	LabelAr      string    `json:"label_ar"`
	SortOrder    int       `json:"sort_order"`
	IsActive     bool      `json:"is_active"`
	RequiresNote bool      `json:"requires_note"`
	CreatedAt    time.Time `json:"created_at"`
}

func (r *LostReason) Normalize() error {
	r.Code = strings.ToLower(strings.TrimSpace(r.Code))
	r.LabelEn = strings.TrimSpace(r.LabelEn)
	r.LabelAr = strings.TrimSpace(r.LabelAr)
	if r.Code == "" {
		return shared.NewValidation("code is required")
	}
	if r.LabelEn == "" && r.LabelAr == "" {
		return shared.NewValidation("label_en or label_ar is required")
	}
	return nil
}

// MessageTemplate is a canned reply snippet (T-224).
type MessageTemplate struct {
	ID            uuid.UUID       `json:"id"`
	BranchID      uuid.UUID       `json:"branch_id"`
	Channel       string          `json:"channel"`
	Name          string          `json:"name"`
	Body          string          `json:"body"`
	VariablesJSON json.RawMessage `json:"variables_json"`
	IsActive      bool            `json:"is_active"`
	CreatedBy     *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (t *MessageTemplate) Normalize() error {
	t.Name = strings.TrimSpace(t.Name)
	t.Body = strings.TrimSpace(t.Body)
	t.Channel = strings.TrimSpace(t.Channel)
	if t.Channel == "" {
		t.Channel = "*"
	}
	if t.Name == "" {
		return shared.NewValidation("name is required")
	}
	if t.Body == "" {
		return shared.NewValidation("body is required")
	}
	if len(t.VariablesJSON) == 0 {
		t.VariablesJSON = json.RawMessage(`[]`)
	}
	return nil
}

// FieldConfig controls form field visibility (T-226).
type FieldConfig struct {
	BranchID      uuid.UUID `json:"branch_id"`
	Entity        string    `json:"entity"`
	FieldKey      string    `json:"field_key"`
	Visible       bool      `json:"visible"`
	Required      bool      `json:"required"`
	SortOrder     int       `json:"sort_order"`
	LabelOverride string    `json:"label_override"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// AlertThresholds are global branch alert knobs (T-227).
type AlertThresholds struct {
	BranchID             uuid.UUID `json:"branch_id"`
	CapacitySoftPct      int       `json:"capacity_soft_pct"`
	PaymentOverdueHours  int       `json:"payment_overdue_hours"`
	MissingDocHours      int       `json:"missing_doc_hours"`
	LeadNoFollowupHours  int       `json:"lead_no_followup_hours"`
	TargetBehindPct      int       `json:"target_behind_pct"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func DefaultAlertThresholds(branchID uuid.UUID) AlertThresholds {
	return AlertThresholds{
		BranchID: branchID, CapacitySoftPct: 80, PaymentOverdueHours: 12,
		MissingDocHours: 24, LeadNoFollowupHours: 24, TargetBehindPct: 15,
		UpdatedAt: time.Now().UTC(),
	}
}

func (a *AlertThresholds) Normalize() error {
	if a.CapacitySoftPct < 1 || a.CapacitySoftPct > 100 {
		return shared.NewValidation("capacity_soft_pct out of range")
	}
	if a.PaymentOverdueHours <= 0 || a.MissingDocHours <= 0 || a.LeadNoFollowupHours <= 0 {
		return shared.NewValidation("hours must be > 0")
	}
	if a.TargetBehindPct < 1 || a.TargetBehindPct > 100 {
		return shared.NewValidation("target_behind_pct out of range")
	}
	return nil
}

// Repository is the admin-config persistence port (ISP: settings only).
type Repository interface {
	ListSLA(ctx context.Context, branchID uuid.UUID) ([]SLAPolicy, error)
	UpsertSLA(ctx context.Context, p *SLAPolicy) error

	ListEscalationOverrides(ctx context.Context, branchID uuid.UUID) ([]EscalationOverride, error)
	UpsertEscalationOverride(ctx context.Context, o *EscalationOverride) error
	DeleteEscalationOverride(ctx context.Context, branchID uuid.UUID, kind string) error

	ListLostReasons(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]LostReason, error)
	UpsertLostReason(ctx context.Context, r *LostReason) error
	DeleteLostReason(ctx context.Context, branchID, id uuid.UUID) error

	ListTemplates(ctx context.Context, branchID uuid.UUID, channel string) ([]MessageTemplate, error)
	GetTemplate(ctx context.Context, branchID, id uuid.UUID) (*MessageTemplate, error)
	InsertTemplate(ctx context.Context, t *MessageTemplate) error
	UpdateTemplate(ctx context.Context, t *MessageTemplate) error
	DeleteTemplate(ctx context.Context, branchID, id uuid.UUID) error

	ListFieldConfigs(ctx context.Context, branchID uuid.UUID, entity string) ([]FieldConfig, error)
	ReplaceFieldConfigs(ctx context.Context, branchID uuid.UUID, entity string, rows []FieldConfig) error

	GetAlertThresholds(ctx context.Context, branchID uuid.UUID) (*AlertThresholds, error)
	UpsertAlertThresholds(ctx context.Context, a *AlertThresholds) error
}
