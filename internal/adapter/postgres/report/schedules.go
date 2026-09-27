package report

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/report"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var branchCol = pgscope.Columns{Branch: "branch_id"}

const scheduleCols = `id, branch_id, kind, frequency, recipient_ids, enabled, created_by, last_run_at, created_at, updated_at`

func scanSchedule(row pgx.Row) (*domain.Schedule, error) {
	var s domain.Schedule
	var kind, freq string
	if err := row.Scan(&s.ID, &s.BranchID, &kind, &freq, &s.RecipientIDs, &s.Enabled,
		&s.CreatedBy, &s.LastRunAt, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.Kind, s.Frequency = domain.Kind(kind), domain.Frequency(freq)
	return &s, nil
}

func (r *Repository) ListSchedules(ctx context.Context, branchID uuid.UUID) ([]domain.Schedule, error) {
	scope, args, err := pgscope.Clause(ctx, branchCol, []any{branchID})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx,
		`SELECT `+scheduleCols+` FROM report_schedules WHERE branch_id=$1`+scope+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	return collectSchedules(rows)
}

func (r *Repository) ListEnabledSchedules(ctx context.Context, limit int) ([]domain.Schedule, error) {
	scope, args, err := pgscope.Clause(ctx, branchCol, []any{limit})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx,
		`SELECT `+scheduleCols+` FROM report_schedules WHERE enabled`+scope+` ORDER BY created_at LIMIT $1`, args...)
	if err != nil {
		return nil, err
	}
	return collectSchedules(rows)
}

func collectSchedules(rows pgx.Rows) ([]domain.Schedule, error) {
	defer rows.Close()
	out := []domain.Schedule{}
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *Repository) GetSchedule(ctx context.Context, id uuid.UUID) (*domain.Schedule, error) {
	scope, args, err := pgscope.Clause(ctx, branchCol, []any{id})
	if err != nil {
		return nil, err
	}
	s, err := scanSchedule(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT `+scheduleCols+` FROM report_schedules WHERE id=$1`+scope, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("report schedule not found")
	}
	return s, err
}

func (r *Repository) InsertSchedule(ctx context.Context, s *domain.Schedule) error {
	if err := pgscope.EnsureBranch(ctx, s.BranchID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO report_schedules (`+scheduleCols+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		s.ID, s.BranchID, string(s.Kind), string(s.Frequency), s.RecipientIDs, s.Enabled,
		s.CreatedBy, s.LastRunAt, s.CreatedAt, s.UpdatedAt)
	return err
}

func (r *Repository) UpdateSchedule(ctx context.Context, s *domain.Schedule) error {
	scope, args, err := pgscope.Clause(ctx, branchCol, []any{
		s.ID, string(s.Kind), string(s.Frequency), s.RecipientIDs, s.Enabled, s.UpdatedAt,
	})
	if err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE report_schedules SET kind=$2, frequency=$3, recipient_ids=$4, enabled=$5, updated_at=$6
		WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("report schedule not found")
	}
	return nil
}

func (r *Repository) DeleteSchedule(ctx context.Context, id uuid.UUID) error {
	scope, args, err := pgscope.Clause(ctx, branchCol, []any{id})
	if err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `DELETE FROM report_schedules WHERE id=$1`+scope, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("report schedule not found")
	}
	return nil
}

func (r *Repository) MarkScheduleRun(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx,
		`UPDATE report_schedules SET last_run_at=$2 WHERE id=$1`, id, at)
	return err
}

func (r *Repository) RunExists(ctx context.Context, scheduleID uuid.UUID, period string) (bool, error) {
	var ok bool
	err := tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM report_runs WHERE schedule_id=$1 AND period=$2)`, scheduleID, period).Scan(&ok)
	return ok, err
}

func (r *Repository) InsertRun(ctx context.Context, run *domain.ScheduledRun) (bool, error) {
	if err := pgscope.EnsureBranch(ctx, run.BranchID); err != nil {
		return false, err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO report_runs (id, schedule_id, branch_id, kind, period, filename, row_count, content, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (schedule_id, period) DO NOTHING`,
		run.ID, run.ScheduleID, run.BranchID, string(run.Kind), run.Period, run.Filename,
		run.RowCount, run.Content, run.CreatedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

const runCols = `id, schedule_id, branch_id, kind, period, filename, row_count, created_at`

func (r *Repository) ListRuns(ctx context.Context, branchID uuid.UUID, scheduleID *uuid.UUID, limit int) ([]domain.ScheduledRun, error) {
	scope, args, err := pgscope.Clause(ctx, branchCol, []any{branchID, scheduleID, limit})
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT `+runCols+` FROM report_runs
		WHERE branch_id=$1 AND ($2::uuid IS NULL OR schedule_id=$2)`+scope+`
		ORDER BY created_at DESC LIMIT $3`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ScheduledRun{}
	for rows.Next() {
		var run domain.ScheduledRun
		var kind string
		if err := rows.Scan(&run.ID, &run.ScheduleID, &run.BranchID, &kind, &run.Period,
			&run.Filename, &run.RowCount, &run.CreatedAt); err != nil {
			return nil, err
		}
		run.Kind = domain.Kind(kind)
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *Repository) GetRun(ctx context.Context, id uuid.UUID) (*domain.ScheduledRun, error) {
	scope, args, err := pgscope.Clause(ctx, branchCol, []any{id})
	if err != nil {
		return nil, err
	}
	var run domain.ScheduledRun
	var kind string
	err = tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT `+runCols+`, content FROM report_runs WHERE id=$1`+scope, args...).
		Scan(&run.ID, &run.ScheduleID, &run.BranchID, &kind, &run.Period, &run.Filename,
			&run.RowCount, &run.CreatedAt, &run.Content)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("report run not found")
	}
	if err != nil {
		return nil, err
	}
	run.Kind = domain.Kind(kind)
	return &run, nil
}
