package importexport

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const jobCols = `id, branch_id, entity_type, mode, status, file_name, content_type,
	COALESCE(file_bytes, ''::bytea), storage_key, headers_json, mapping_json, preview_json,
	total_rows, success_count, failed_count, skipped_count, rollback_token,
	error_message, created_by, created_at, updated_at`

func scanJob(scan func(dest ...any) error) (*domain.ImportJob, error) {
	var j domain.ImportJob
	var entity, mode, status string
	var headersRaw, mappingRaw, previewRaw []byte
	err := scan(
		&j.ID, &j.BranchID, &entity, &mode, &status, &j.FileName, &j.ContentType,
		&j.FileBytes, &j.StorageKey, &headersRaw, &mappingRaw, &previewRaw,
		&j.TotalRows, &j.SuccessCount, &j.FailedCount, &j.SkippedCount, &j.RollbackToken,
		&j.ErrorMessage, &j.CreatedBy, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	j.EntityType = domain.EntityType(entity)
	j.Mode = domain.ImportMode(mode)
	j.Status = domain.JobStatus(status)
	_ = json.Unmarshal(headersRaw, &j.Headers)
	_ = json.Unmarshal(mappingRaw, &j.Mapping)
	_ = json.Unmarshal(previewRaw, &j.PreviewRows)
	if j.Headers == nil {
		j.Headers = []string{}
	}
	if j.Mapping == nil {
		j.Mapping = map[string]string{}
	}
	if j.PreviewRows == nil {
		j.PreviewRows = [][]string{}
	}
	return &j, nil
}

func (r *Repository) CreateJob(ctx context.Context, j *domain.ImportJob) error {
	q := tx.QuerierFrom(ctx, r.pool)
	headers, _ := json.Marshal(j.Headers)
	mapping, _ := json.Marshal(j.Mapping)
	preview, _ := json.Marshal(j.PreviewRows)
	_, err := q.Exec(ctx, `
		INSERT INTO import_jobs (
			id, branch_id, entity_type, mode, status, file_name, content_type,
			file_bytes, storage_key, headers_json, mapping_json, preview_json,
			total_rows, success_count, failed_count, skipped_count, rollback_token,
			error_message, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		j.ID, j.BranchID, string(j.EntityType), string(j.Mode), string(j.Status),
		j.FileName, j.ContentType, j.FileBytes, j.StorageKey, headers, mapping, preview,
		j.TotalRows, j.SuccessCount, j.FailedCount, j.SkippedCount, j.RollbackToken,
		j.ErrorMessage, j.CreatedBy, j.CreatedAt, j.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateJob(ctx context.Context, j *domain.ImportJob) error {
	q := tx.QuerierFrom(ctx, r.pool)
	headers, _ := json.Marshal(j.Headers)
	mapping, _ := json.Marshal(j.Mapping)
	preview, _ := json.Marshal(j.PreviewRows)
	_, err := q.Exec(ctx, `
		UPDATE import_jobs SET
			mode=$2, status=$3, file_name=$4, content_type=$5, file_bytes=$6, storage_key=$7,
			headers_json=$8, mapping_json=$9, preview_json=$10,
			total_rows=$11, success_count=$12, failed_count=$13, skipped_count=$14,
			error_message=$15, updated_at=$16
		WHERE id=$1`,
		j.ID, string(j.Mode), string(j.Status), j.FileName, j.ContentType, j.FileBytes, j.StorageKey,
		headers, mapping, preview,
		j.TotalRows, j.SuccessCount, j.FailedCount, j.SkippedCount,
		j.ErrorMessage, j.UpdatedAt,
	)
	return err
}

func (r *Repository) GetJob(ctx context.Context, id uuid.UUID) (*domain.ImportJob, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `SELECT `+jobCols+` FROM import_jobs WHERE id=$1`, id)
	j, err := scanJob(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (r *Repository) ListJobs(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.ImportJob, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+jobCols+` FROM import_jobs
		WHERE branch_id=$1 ORDER BY created_at DESC LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ImportJob
	for rows.Next() {
		j, err := scanJob(rows.Scan)
		if err != nil {
			return nil, err
		}
		// omit file bytes in list payloads (still loaded; clear for memory)
		j.FileBytes = nil
		out = append(out, *j)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceRowErrors(ctx context.Context, jobID uuid.UUID, errs []domain.RowError) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM import_row_errors WHERE job_id=$1`, jobID); err != nil {
		return err
	}
	for i := range errs {
		e := &errs[i]
		if e.ID == uuid.Nil {
			e.ID = uuid.New()
		}
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Now().UTC()
		}
		if len(e.RawJSON) == 0 {
			e.RawJSON = []byte(`{}`)
		}
		if _, err := q.Exec(ctx, `
			INSERT INTO import_row_errors (id, job_id, row_number, field, message, raw_json, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			e.ID, jobID, e.RowNumber, e.Field, e.Message, e.RawJSON, e.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListRowErrors(ctx context.Context, jobID uuid.UUID) ([]domain.RowError, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, job_id, row_number, field, message, raw_json, created_at
		FROM import_row_errors WHERE job_id=$1 ORDER BY row_number, created_at`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RowError
	for rows.Next() {
		var e domain.RowError
		if err := rows.Scan(&e.ID, &e.JobID, &e.RowNumber, &e.Field, &e.Message, &e.RawJSON, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *Repository) CreateTemplate(ctx context.Context, t *domain.MappingTemplate) error {
	q := tx.QuerierFrom(ctx, r.pool)
	mapping, _ := json.Marshal(t.Mapping)
	_, err := q.Exec(ctx, `
		INSERT INTO import_mapping_templates (id, branch_id, name, entity_type, mapping_json, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		t.ID, t.BranchID, t.Name, string(t.EntityType), mapping, t.CreatedBy, t.CreatedAt,
	)
	return err
}

func (r *Repository) ListTemplates(ctx context.Context, branchID uuid.UUID, entityType domain.EntityType) ([]domain.MappingTemplate, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var rows pgx.Rows
	var err error
	if entityType == "" {
		rows, err = q.Query(ctx, `
			SELECT id, branch_id, name, entity_type, mapping_json, created_by, created_at
			FROM import_mapping_templates WHERE branch_id=$1 ORDER BY name`, branchID)
	} else {
		rows, err = q.Query(ctx, `
			SELECT id, branch_id, name, entity_type, mapping_json, created_by, created_at
			FROM import_mapping_templates WHERE branch_id=$1 AND entity_type=$2 ORDER BY name`,
			branchID, string(entityType))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.MappingTemplate
	for rows.Next() {
		var t domain.MappingTemplate
		var et string
		var mappingRaw []byte
		if err := rows.Scan(&t.ID, &t.BranchID, &t.Name, &et, &mappingRaw, &t.CreatedBy, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.EntityType = domain.EntityType(et)
		_ = json.Unmarshal(mappingRaw, &t.Mapping)
		if t.Mapping == nil {
			t.Mapping = map[string]string{}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) DeleteTemplate(ctx context.Context, id, branchID uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	tag, err := q.Exec(ctx, `DELETE FROM import_mapping_templates WHERE id=$1 AND branch_id=$2`, id, branchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("import_template")
	}
	return nil
}

func (r *Repository) ExportCustomers(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.ExportRow, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT full_name, COALESCE(full_name_ar,''), phone, COALESCE(email,''),
			COALESCE(nationality,''), COALESCE(passport_no,''),
			COALESCE(to_char(date_of_birth,'YYYY-MM-DD'),''),
			COALESCE(notes,''), COALESCE(special_requirements,'')
		FROM customers
		WHERE branch_id=$1 AND is_active=true AND merged_into_id IS NULL
		ORDER BY created_at DESC LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ExportRow
	for rows.Next() {
		var fullName, fullNameAR, phone, email, nationality, passport, dob, notes, special string
		if err := rows.Scan(&fullName, &fullNameAR, &phone, &email, &nationality, &passport, &dob, &notes, &special); err != nil {
			return nil, err
		}
		out = append(out, domain.ExportRow{
			"full_name": fullName, "full_name_ar": fullNameAR, "phone": phone, "email": email,
			"nationality": nationality, "passport_no": passport, "date_of_birth": dob,
			"notes": notes, "special_requirements": special,
		})
	}
	return out, rows.Err()
}

func (r *Repository) ExportBookings(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.ExportRow, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT COALESCE(c.phone,''), COALESCE(d.code,''), b.status::text, b.pax_count,
			b.total_amount::text, COALESCE(b.currency,''), COALESCE(b.notes,'')
		FROM bookings b
		LEFT JOIN customers c ON c.id = b.customer_id
		LEFT JOIN departures d ON d.id = b.departure_id
		WHERE b.branch_id=$1
		ORDER BY b.created_at DESC LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ExportRow
	for rows.Next() {
		var phone, depCode, status, total, currency, notes string
		var pax int
		if err := rows.Scan(&phone, &depCode, &status, &pax, &total, &currency, &notes); err != nil {
			return nil, err
		}
		out = append(out, domain.ExportRow{
			"customer_phone": phone, "departure_code": depCode, "status": status,
			"pax_count": itoa(pax), "total_amount": total, "currency": currency, "notes": notes,
		})
	}
	return out, rows.Err()
}

func (r *Repository) ExportPayments(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.ExportRow, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT p.booking_id::text, p.amount::text, COALESCE(p.currency,''),
			COALESCE(p.method,''), COALESCE(p.reference,''), p.status::text, COALESCE(p.note,'')
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id
		WHERE b.branch_id=$1
		ORDER BY p.created_at DESC LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ExportRow
	for rows.Next() {
		var bookingID, amount, currency, method, ref, status, note string
		if err := rows.Scan(&bookingID, &amount, &currency, &method, &ref, &status, &note); err != nil {
			return nil, err
		}
		out = append(out, domain.ExportRow{
			"booking_id": bookingID, "amount": amount, "currency": currency,
			"method": method, "reference": ref, "status": status, "note": note,
		})
	}
	return out, rows.Err()
}

func (r *Repository) ExportDepartures(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.ExportRow, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT COALESCE(pkg.code,''), d.code,
			to_char(d.depart_date,'YYYY-MM-DD'), COALESCE(to_char(d.return_date,'YYYY-MM-DD'),''),
			d.capacity_total, d.base_price::text, COALESCE(d.currency,'')
		FROM departures d
		JOIN packages pkg ON pkg.id = d.package_id
		WHERE pkg.branch_id=$1
		ORDER BY d.depart_date DESC LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ExportRow
	for rows.Next() {
		var pkgCode, code, depart, ret, price, currency string
		var cap int
		if err := rows.Scan(&pkgCode, &code, &depart, &ret, &cap, &price, &currency); err != nil {
			return nil, err
		}
		out = append(out, domain.ExportRow{
			"package_code": pkgCode, "code": code, "depart_date": depart, "return_date": ret,
			"capacity_total": itoa(cap), "base_price": price, "currency": currency,
		})
	}
	return out, rows.Err()
}

func itoa(n int) string { return strconv.Itoa(n) }
