package adminconfig

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/adminconfig"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Service struct {
	repo domain.Repository
	tx   tx.Runner
	now  func() time.Time
}

func NewService(repo domain.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, tx: txm, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) GetSLA(ctx context.Context, branchID uuid.UUID) ([]domain.SLAPolicy, error) {
	return s.repo.ListSLA(ctx, branchID)
}

type UpsertSLAInput struct {
	Channel              string `json:"channel"`
	FirstResponseSeconds int    `json:"first_response_seconds"`
}

func (s *Service) PutSLA(ctx context.Context, branchID uuid.UUID, items []UpsertSLAInput) ([]domain.SLAPolicy, error) {
	if branchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	if len(items) == 0 {
		return nil, shared.NewValidation("at least one sla policy is required")
	}
	out := make([]domain.SLAPolicy, 0, len(items))
	err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		for _, in := range items {
			p := domain.SLAPolicy{
				BranchID: branchID, Channel: in.Channel, FirstResponseSeconds: in.FirstResponseSeconds,
			}
			if err := p.Normalize(); err != nil {
				return err
			}
			if err := s.repo.UpsertSLA(txCtx, &p); err != nil {
				return err
			}
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

type EscalationRuleDTO struct {
	Kind                 string        `json:"kind"`
	Severity             string        `json:"severity"`
	EscalateAfterSeconds int           `json:"escalate_after_seconds"`
	EscalateToRoles      []string      `json:"escalate_to_roles"`
	Groupable            bool          `json:"groupable"`
	DefaultTitle         string        `json:"default_title"`
	DefaultHref          string        `json:"default_href"`
	EntityType           string        `json:"entity_type"`
	Overridden           bool          `json:"overridden"`
	Enabled              bool          `json:"enabled"`
}

func (s *Service) GetEscalationMatrix(ctx context.Context, branchID uuid.UUID) ([]EscalationRuleDTO, error) {
	overlays, err := s.repo.ListEscalationOverrides(ctx, branchID)
	if err != nil {
		return nil, err
	}
	byKind := map[string]domain.EscalationOverride{}
	for _, o := range overlays {
		byKind[o.Kind] = o
	}
	merged := domain.MergeEscalation(notification.DefaultRules(), overlays)
	out := make([]EscalationRuleDTO, 0, len(merged))
	for _, r := range merged {
		_, overridden := byKind[r.Kind]
		out = append(out, EscalationRuleDTO{
			Kind: r.Kind, Severity: string(r.Severity),
			EscalateAfterSeconds: int(r.EscalateAfter / time.Second),
			EscalateToRoles: append([]string(nil), r.EscalateToRoles...),
			Groupable: r.Groupable, DefaultTitle: r.DefaultTitle,
			DefaultHref: r.DefaultHref, EntityType: r.EntityType,
			Overridden: overridden, Enabled: true,
		})
	}
	// Surface explicitly disabled overlays so admins can re-enable.
	for _, o := range overlays {
		if o.Enabled {
			continue
		}
		out = append(out, EscalationRuleDTO{
			Kind: o.Kind, EscalateAfterSeconds: o.EscalateAfterSeconds,
			EscalateToRoles: append([]string(nil), o.EscalateToRoles...),
			Overridden: true, Enabled: false,
		})
	}
	return out, nil
}

type UpsertEscalationInput struct {
	EscalateAfterSeconds int      `json:"escalate_after_seconds"`
	EscalateToRoles      []string `json:"escalate_to_roles"`
	Enabled              *bool    `json:"enabled"`
}

func (s *Service) UpsertEscalation(ctx context.Context, branchID uuid.UUID, kind string, in UpsertEscalationInput) (*domain.EscalationOverride, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, shared.NewValidation("kind is required")
	}
	if in.EscalateAfterSeconds < 0 {
		return nil, shared.NewValidation("escalate_after_seconds must be >= 0")
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	o := &domain.EscalationOverride{
		BranchID: branchID, Kind: kind,
		EscalateAfterSeconds: in.EscalateAfterSeconds,
		EscalateToRoles: append([]string(nil), in.EscalateToRoles...),
		Enabled: enabled, UpdatedAt: s.now(),
	}
	if err := s.repo.UpsertEscalationOverride(ctx, o); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *Service) DeleteEscalation(ctx context.Context, branchID uuid.UUID, kind string) error {
	if err := s.repo.DeleteEscalationOverride(ctx, branchID, strings.TrimSpace(kind)); err != nil {
		return shared.NewNotFound("escalation_override")
	}
	return nil
}

func (s *Service) GetLostReasons(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.LostReason, error) {
	items, err := s.repo.ListLostReasons(ctx, branchID, activeOnly)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return items, nil
	}
	// Fallback seed when branch has no taxonomy rows yet (T-223).
	codes := lead.LostReasonCodes()
	out := make([]domain.LostReason, 0, len(codes))
	now := s.now()
	for i, code := range codes {
		out = append(out, domain.LostReason{
			ID: uuid.Nil, BranchID: branchID, Code: code,
			LabelEn: strings.ReplaceAll(code, "_", " "), LabelAr: "",
			SortOrder: (i + 1) * 10, IsActive: true,
			RequiresNote: code == lead.LostOther, CreatedAt: now,
		})
	}
	return out, nil
}

type LostReasonInput struct {
	Code         string `json:"code"`
	LabelEn      string `json:"label_en"`
	LabelAr      string `json:"label_ar"`
	SortOrder    int    `json:"sort_order"`
	IsActive     *bool  `json:"is_active"`
	RequiresNote bool   `json:"requires_note"`
}

func (s *Service) CreateLostReason(ctx context.Context, branchID uuid.UUID, in LostReasonInput) (*domain.LostReason, error) {
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	lr := &domain.LostReason{
		ID: uuid.New(), BranchID: branchID, Code: in.Code,
		LabelEn: in.LabelEn, LabelAr: in.LabelAr, SortOrder: in.SortOrder,
		IsActive: active, RequiresNote: in.RequiresNote, CreatedAt: s.now(),
	}
	if err := lr.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertLostReason(ctx, lr); err != nil {
		return nil, err
	}
	return lr, nil
}

func (s *Service) UpdateLostReason(ctx context.Context, branchID, id uuid.UUID, in LostReasonInput) (*domain.LostReason, error) {
	items, err := s.repo.ListLostReasons(ctx, branchID, false)
	if err != nil {
		return nil, err
	}
	var found *domain.LostReason
	for i := range items {
		if items[i].ID == id {
			found = &items[i]
			break
		}
	}
	if found == nil {
		return nil, shared.NewNotFound("lost_reason")
	}
	if in.Code != "" {
		found.Code = in.Code
	}
	if in.LabelEn != "" || in.LabelAr != "" {
		found.LabelEn = in.LabelEn
		found.LabelAr = in.LabelAr
	}
	found.SortOrder = in.SortOrder
	found.RequiresNote = in.RequiresNote
	if in.IsActive != nil {
		found.IsActive = *in.IsActive
	}
	if err := found.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertLostReason(ctx, found); err != nil {
		return nil, err
	}
	return found, nil
}

func (s *Service) DeleteLostReason(ctx context.Context, branchID, id uuid.UUID) error {
	if err := s.repo.DeleteLostReason(ctx, branchID, id); err != nil {
		return shared.NewNotFound("lost_reason")
	}
	return nil
}

type TemplateInput struct {
	Channel       string `json:"channel"`
	Name          string `json:"name"`
	Body          string `json:"body"`
	VariablesJSON []byte `json:"variables_json"`
	IsActive      *bool  `json:"is_active"`
}

func (s *Service) ListTemplates(ctx context.Context, branchID uuid.UUID, channel string) ([]domain.MessageTemplate, error) {
	return s.repo.ListTemplates(ctx, branchID, channel)
}

func (s *Service) GetTemplate(ctx context.Context, branchID, id uuid.UUID) (*domain.MessageTemplate, error) {
	t, err := s.repo.GetTemplate(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("message_template")
	}
	return t, nil
}

func (s *Service) CreateTemplate(ctx context.Context, branchID, actorID uuid.UUID, in TemplateInput) (*domain.MessageTemplate, error) {
	now := s.now()
	active := true
	if in.IsActive != nil {
		active = *in.IsActive
	}
	actor := actorID
	t := &domain.MessageTemplate{
		ID: uuid.New(), BranchID: branchID, Channel: in.Channel,
		Name: in.Name, Body: in.Body, VariablesJSON: in.VariablesJSON,
		IsActive: active, CreatedBy: &actor, CreatedAt: now, UpdatedAt: now,
	}
	if err := t.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.InsertTemplate(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) PatchTemplate(ctx context.Context, branchID, id uuid.UUID, in TemplateInput) (*domain.MessageTemplate, error) {
	t, err := s.repo.GetTemplate(ctx, branchID, id)
	if err != nil {
		return nil, shared.NewNotFound("message_template")
	}
	if in.Channel != "" {
		t.Channel = in.Channel
	}
	if in.Name != "" {
		t.Name = in.Name
	}
	if in.Body != "" {
		t.Body = in.Body
	}
	if len(in.VariablesJSON) > 0 {
		t.VariablesJSON = in.VariablesJSON
	}
	if in.IsActive != nil {
		t.IsActive = *in.IsActive
	}
	if err := t.Normalize(); err != nil {
		return nil, err
	}
	t.UpdatedAt = s.now()
	if err := s.repo.UpdateTemplate(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) DeleteTemplate(ctx context.Context, branchID, id uuid.UUID) error {
	if err := s.repo.DeleteTemplate(ctx, branchID, id); err != nil {
		return shared.NewNotFound("message_template")
	}
	return nil
}

func (s *Service) GetFields(ctx context.Context, branchID uuid.UUID, entity string) ([]domain.FieldConfig, error) {
	entity = strings.TrimSpace(entity)
	if entity == "" {
		return nil, shared.NewValidation("entity query param is required")
	}
	return s.repo.ListFieldConfigs(ctx, branchID, entity)
}

func (s *Service) PutFields(ctx context.Context, branchID uuid.UUID, entity string, rows []domain.FieldConfig) ([]domain.FieldConfig, error) {
	entity = strings.TrimSpace(entity)
	if entity == "" {
		return nil, shared.NewValidation("entity is required")
	}
	now := s.now()
	for i := range rows {
		rows[i].BranchID = branchID
		rows[i].Entity = entity
		rows[i].FieldKey = strings.TrimSpace(rows[i].FieldKey)
		if rows[i].FieldKey == "" {
			return nil, shared.NewValidation("field_key is required")
		}
		rows[i].UpdatedAt = now
	}
	if err := s.tx.WithinTransaction(ctx, func(txCtx context.Context) error {
		return s.repo.ReplaceFieldConfigs(txCtx, branchID, entity, rows)
	}); err != nil {
		return nil, err
	}
	return s.repo.ListFieldConfigs(ctx, branchID, entity)
}

func (s *Service) GetThresholds(ctx context.Context, branchID uuid.UUID) (*domain.AlertThresholds, error) {
	a, err := s.repo.GetAlertThresholds(ctx, branchID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		d := domain.DefaultAlertThresholds(branchID)
		return &d, nil
	}
	return a, nil
}

func (s *Service) PutThresholds(ctx context.Context, branchID uuid.UUID, in domain.AlertThresholds) (*domain.AlertThresholds, error) {
	in.BranchID = branchID
	in.UpdatedAt = s.now()
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertAlertThresholds(ctx, &in); err != nil {
		return nil, err
	}
	return &in, nil
}
