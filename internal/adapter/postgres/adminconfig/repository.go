package adminconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/adminconfig"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) ListSLA(ctx context.Context, branchID uuid.UUID) ([]domain.SLAPolicy, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, channel, first_response_seconds
		FROM sla_policies WHERE branch_id=$1
		ORDER BY channel`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SLAPolicy
	for rows.Next() {
		var p domain.SLAPolicy
		if err := rows.Scan(&p.ID, &p.BranchID, &p.Channel, &p.FirstResponseSeconds); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertSLA(ctx context.Context, p *domain.SLAPolicy) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return q.QueryRow(ctx, `
		INSERT INTO sla_policies (id, branch_id, channel, first_response_seconds)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (branch_id, channel) DO UPDATE SET
			first_response_seconds = EXCLUDED.first_response_seconds
		RETURNING id`,
		p.ID, p.BranchID, p.Channel, p.FirstResponseSeconds,
	).Scan(&p.ID)
}

func (r *Repository) ListEscalationOverrides(ctx context.Context, branchID uuid.UUID) ([]domain.EscalationOverride, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT branch_id, kind, escalate_after_seconds, escalate_to_roles, enabled, updated_at
		FROM escalation_rule_overrides WHERE branch_id=$1
		ORDER BY kind`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EscalationOverride
	for rows.Next() {
		var o domain.EscalationOverride
		if err := rows.Scan(&o.BranchID, &o.Kind, &o.EscalateAfterSeconds, &o.EscalateToRoles, &o.Enabled, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertEscalationOverride(ctx context.Context, o *domain.EscalationOverride) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO escalation_rule_overrides (
			branch_id, kind, escalate_after_seconds, escalate_to_roles, enabled, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (branch_id, kind) DO UPDATE SET
			escalate_after_seconds = EXCLUDED.escalate_after_seconds,
			escalate_to_roles = EXCLUDED.escalate_to_roles,
			enabled = EXCLUDED.enabled,
			updated_at = EXCLUDED.updated_at`,
		o.BranchID, o.Kind, o.EscalateAfterSeconds, o.EscalateToRoles, o.Enabled, o.UpdatedAt,
	)
	return err
}

func (r *Repository) DeleteEscalationOverride(ctx context.Context, branchID uuid.UUID, kind string) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		DELETE FROM escalation_rule_overrides WHERE branch_id=$1 AND kind=$2`, branchID, kind)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) ListLostReasons(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.LostReason, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	query := `
		SELECT id, branch_id, code, label_en, label_ar, sort_order, is_active, requires_note, created_at
		FROM lost_reason_codes WHERE branch_id=$1`
	if activeOnly {
		query += ` AND is_active=TRUE`
	}
	query += ` ORDER BY sort_order, code`
	rows, err := q.Query(ctx, query, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LostReason
	for rows.Next() {
		var lr domain.LostReason
		if err := rows.Scan(
			&lr.ID, &lr.BranchID, &lr.Code, &lr.LabelEn, &lr.LabelAr,
			&lr.SortOrder, &lr.IsActive, &lr.RequiresNote, &lr.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertLostReason(ctx context.Context, lr *domain.LostReason) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if lr.ID == uuid.Nil {
		lr.ID = uuid.New()
	}
	return q.QueryRow(ctx, `
		INSERT INTO lost_reason_codes (
			id, branch_id, code, label_en, label_ar, sort_order, is_active, requires_note, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (branch_id, code) DO UPDATE SET
			label_en = EXCLUDED.label_en,
			label_ar = EXCLUDED.label_ar,
			sort_order = EXCLUDED.sort_order,
			is_active = EXCLUDED.is_active,
			requires_note = EXCLUDED.requires_note
		RETURNING id`,
		lr.ID, lr.BranchID, lr.Code, lr.LabelEn, lr.LabelAr,
		lr.SortOrder, lr.IsActive, lr.RequiresNote, lr.CreatedAt,
	).Scan(&lr.ID)
}

func (r *Repository) DeleteLostReason(ctx context.Context, branchID, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		DELETE FROM lost_reason_codes WHERE branch_id=$1 AND id=$2`, branchID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) ListTemplates(ctx context.Context, branchID uuid.UUID, channel string) ([]domain.MessageTemplate, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, channel, name, body, variables_json, is_active, created_by, created_at, updated_at
		FROM message_templates
		WHERE branch_id=$1 AND ($2 = '' OR channel=$2 OR channel='*')
		ORDER BY name`, branchID, channel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTemplates(rows)
}

func (r *Repository) GetTemplate(ctx context.Context, branchID, id uuid.UUID) (*domain.MessageTemplate, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	t, err := scanTemplate(q.QueryRow(ctx, `
		SELECT id, branch_id, channel, name, body, variables_json, is_active, created_by, created_at, updated_at
		FROM message_templates WHERE branch_id=$1 AND id=$2`, branchID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return t, err
}

func (r *Repository) InsertTemplate(ctx context.Context, t *domain.MessageTemplate) error {
	q := tx.QuerierFrom(ctx, r.pool)
	vars := t.VariablesJSON
	if len(vars) == 0 {
		vars = json.RawMessage(`[]`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO message_templates (
			id, branch_id, channel, name, body, variables_json, is_active, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		t.ID, t.BranchID, t.Channel, t.Name, t.Body, vars, t.IsActive, t.CreatedBy, t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateTemplate(ctx context.Context, t *domain.MessageTemplate) error {
	q := tx.QuerierFrom(ctx, r.pool)
	vars := t.VariablesJSON
	if len(vars) == 0 {
		vars = json.RawMessage(`[]`)
	}
	ct, err := q.Exec(ctx, `
		UPDATE message_templates SET
			channel=$3, name=$4, body=$5, variables_json=$6, is_active=$7, updated_at=$8
		WHERE branch_id=$1 AND id=$2`,
		t.BranchID, t.ID, t.Channel, t.Name, t.Body, vars, t.IsActive, t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) DeleteTemplate(ctx context.Context, branchID, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		DELETE FROM message_templates WHERE branch_id=$1 AND id=$2`, branchID, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) ListFieldConfigs(ctx context.Context, branchID uuid.UUID, entity string) ([]domain.FieldConfig, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT branch_id, entity, field_key, visible, required, sort_order, label_override, updated_at
		FROM entity_field_configs
		WHERE branch_id=$1 AND ($2 = '' OR entity=$2)
		ORDER BY entity, sort_order, field_key`, branchID, entity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FieldConfig
	for rows.Next() {
		var f domain.FieldConfig
		if err := rows.Scan(
			&f.BranchID, &f.Entity, &f.FieldKey, &f.Visible, &f.Required,
			&f.SortOrder, &f.LabelOverride, &f.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceFieldConfigs(ctx context.Context, branchID uuid.UUID, entity string, rows []domain.FieldConfig) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `
		DELETE FROM entity_field_configs WHERE branch_id=$1 AND entity=$2`, branchID, entity); err != nil {
		return err
	}
	for i := range rows {
		f := &rows[i]
		f.BranchID = branchID
		f.Entity = entity
		if _, err := q.Exec(ctx, `
			INSERT INTO entity_field_configs (
				branch_id, entity, field_key, visible, required, sort_order, label_override, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			f.BranchID, f.Entity, f.FieldKey, f.Visible, f.Required, f.SortOrder, f.LabelOverride, f.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) GetAlertThresholds(ctx context.Context, branchID uuid.UUID) (*domain.AlertThresholds, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var a domain.AlertThresholds
	err := q.QueryRow(ctx, `
		SELECT branch_id, capacity_soft_pct, payment_overdue_hours, missing_doc_hours,
			lead_no_followup_hours, target_behind_pct, updated_at
		FROM alert_threshold_settings WHERE branch_id=$1`, branchID).Scan(
		&a.BranchID, &a.CapacitySoftPct, &a.PaymentOverdueHours, &a.MissingDocHours,
		&a.LeadNoFollowupHours, &a.TargetBehindPct, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *Repository) UpsertAlertThresholds(ctx context.Context, a *domain.AlertThresholds) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO alert_threshold_settings (
			branch_id, capacity_soft_pct, payment_overdue_hours, missing_doc_hours,
			lead_no_followup_hours, target_behind_pct, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (branch_id) DO UPDATE SET
			capacity_soft_pct = EXCLUDED.capacity_soft_pct,
			payment_overdue_hours = EXCLUDED.payment_overdue_hours,
			missing_doc_hours = EXCLUDED.missing_doc_hours,
			lead_no_followup_hours = EXCLUDED.lead_no_followup_hours,
			target_behind_pct = EXCLUDED.target_behind_pct,
			updated_at = EXCLUDED.updated_at`,
		a.BranchID, a.CapacitySoftPct, a.PaymentOverdueHours, a.MissingDocHours,
		a.LeadNoFollowupHours, a.TargetBehindPct, a.UpdatedAt,
	)
	return err
}

func scanTemplate(row pgx.Row) (*domain.MessageTemplate, error) {
	var t domain.MessageTemplate
	var vars []byte
	err := row.Scan(
		&t.ID, &t.BranchID, &t.Channel, &t.Name, &t.Body, &vars,
		&t.IsActive, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(vars) == 0 {
		t.VariablesJSON = json.RawMessage(`[]`)
	} else {
		t.VariablesJSON = vars
	}
	return &t, nil
}

func scanTemplates(rows pgx.Rows) ([]domain.MessageTemplate, error) {
	var out []domain.MessageTemplate
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}
