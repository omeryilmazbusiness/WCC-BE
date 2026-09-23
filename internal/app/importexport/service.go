package importexport

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/importexport"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

const batchSize = 50

// QueueMode reports whether the queue is memory or redis (optional DIP).
type QueueMode interface {
	Mode() string
}

type UploadInput struct {
	BranchID    uuid.UUID
	ActorID     uuid.UUID
	EntityType  domain.EntityType
	Mode        domain.ImportMode
	FileName    string
	ContentType string
	FileBytes   []byte
	StorageKey  string
}

type MappingInput struct {
	JobID    uuid.UUID
	BranchID uuid.UUID
	Mapping  map[string]string
	Mode     domain.ImportMode
}

type TemplateInput struct {
	BranchID   uuid.UUID
	ActorID    uuid.UUID
	Name       string
	EntityType domain.EntityType
	Mapping    map[string]string
}

type ExportInput struct {
	BranchID   uuid.UUID
	EntityType domain.EntityType
	Limit      int
}

type Service struct {
	repo      domain.Repository
	customers customer.Repository
	tx        tx.Runner
	queue     shared.Enqueuer
	qMode     QueueMode
}

func NewService(repo domain.Repository, customers customer.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, customers: customers, tx: txm}
}

func (s *Service) SetEnqueuer(q shared.Enqueuer) { s.queue = q }
func (s *Service) SetQueueMode(m QueueMode)       { s.qMode = m }

func (s *Service) Upload(ctx context.Context, in UploadInput) (*domain.ImportJob, error) {
	if !domain.ValidEntityType(in.EntityType) {
		return nil, shared.NewValidation("entity_type must be customers|bookings|payments|departures")
	}
	mode := in.Mode
	if mode == "" {
		mode = domain.ModeUpsert
	}
	if !domain.ValidMode(mode) {
		return nil, shared.NewValidation("mode must be create|update|upsert")
	}
	if len(in.FileBytes) == 0 {
		return nil, shared.NewValidation("file is required")
	}
	sheet, err := domain.ParseFile(in.FileName, in.ContentType, in.FileBytes)
	if err != nil {
		return nil, shared.NewValidation(err.Error())
	}
	now := time.Now().UTC()
	job := &domain.ImportJob{
		ID:            uuid.New(),
		BranchID:      in.BranchID,
		EntityType:    in.EntityType,
		Mode:          mode,
		Status:        domain.StatusUploaded,
		FileName:      in.FileName,
		ContentType:   in.ContentType,
		FileBytes:     in.FileBytes,
		StorageKey:    in.StorageKey,
		Headers:       sheet.Headers,
		Mapping:       domain.SuggestMapping(sheet.Headers),
		PreviewRows:   domain.PreviewRows(sheet.Rows),
		TotalRows:     len(sheet.Rows),
		RollbackToken: uuid.New(),
		CreatedBy:     in.ActorID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.CreateJob(ctx, job); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *Service) Get(ctx context.Context, id, branchID uuid.UUID) (*domain.ImportJob, error) {
	j, err := s.repo.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if j == nil || j.BranchID != branchID {
		return nil, shared.NewNotFound("import_job")
	}
	return j, nil
}

func (s *Service) List(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.ImportJob, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.repo.ListJobs(ctx, branchID, limit)
}

func (s *Service) SetMapping(ctx context.Context, in MappingInput) (*domain.ImportJob, error) {
	j, err := s.Get(ctx, in.JobID, in.BranchID)
	if err != nil {
		return nil, err
	}
	switch j.Status {
	case domain.StatusUploaded, domain.StatusMapped, domain.StatusValidated:
	default:
		return nil, shared.NewInvalidState("cannot change mapping in status " + string(j.Status))
	}
	if len(in.Mapping) == 0 {
		return nil, shared.NewValidation("mapping is required")
	}
	j.Mapping = in.Mapping
	if in.Mode != "" {
		if !domain.ValidMode(in.Mode) {
			return nil, shared.NewValidation("mode must be create|update|upsert")
		}
		j.Mode = in.Mode
	}
	j.Status = domain.StatusMapped
	j.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateJob(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Service) Validate(ctx context.Context, jobID, branchID uuid.UUID) (*domain.ImportJob, error) {
	j, err := s.Get(ctx, jobID, branchID)
	if err != nil {
		return nil, err
	}
	switch j.Status {
	case domain.StatusMapped, domain.StatusValidated, domain.StatusUploaded:
	default:
		return nil, shared.NewInvalidState("cannot validate in status " + string(j.Status))
	}
	if len(j.Mapping) == 0 {
		return nil, shared.NewValidation("mapping is required before validate")
	}
	sheet, err := domain.ParseFile(j.FileName, j.ContentType, j.FileBytes)
	if err != nil {
		return nil, shared.NewValidation(err.Error())
	}
	var rowErrs []domain.RowError
	for i, row := range sheet.Rows {
		vals := domain.MapRow(sheet.Headers, row, j.Mapping)
		_, ferrs := domain.ValidateMappedRow(j.EntityType, vals)
		for field, msg := range ferrs {
			raw, _ := json.Marshal(vals)
			rowErrs = append(rowErrs, domain.RowError{
				ID: uuid.New(), JobID: j.ID, RowNumber: i + 2, Field: field,
				Message: msg, RawJSON: raw, CreatedAt: time.Now().UTC(),
			})
		}
	}
	if err := s.repo.ReplaceRowErrors(ctx, j.ID, rowErrs); err != nil {
		return nil, err
	}
	j.TotalRows = len(sheet.Rows)
	j.FailedCount = len(rowErrs)
	j.Status = domain.StatusValidated
	j.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateJob(ctx, j); err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Service) Confirm(ctx context.Context, jobID, branchID uuid.UUID) (*domain.ImportJob, error) {
	j, err := s.Get(ctx, jobID, branchID)
	if err != nil {
		return nil, err
	}
	switch j.Status {
	case domain.StatusValidated, domain.StatusMapped, domain.StatusQueued:
	default:
		return nil, shared.NewInvalidState("confirm requires validated (or mapped) job; got " + string(j.Status))
	}
	j.Status = domain.StatusQueued
	j.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateJob(ctx, j); err != nil {
		return nil, err
	}

	if s.queue != nil {
		payload, _ := json.Marshal(map[string]string{"import_job_id": j.ID.String()})
		_, _ = s.queue.Enqueue(ctx, shared.JobImportProcess, payload, shared.EnqueueOpts{
			Queue: "default", UniqueKey: "import:" + j.ID.String(),
		})
	}

	// Always process sync after enqueue attempt (idempotent). Memory queues never run a worker.
	if err := s.Process(ctx, j.ID); err != nil {
		return nil, err
	}
	return s.Get(ctx, jobID, branchID)
}

// Process is idempotent: safe for worker + sync confirm.
func (s *Service) Process(ctx context.Context, jobID uuid.UUID) error {
	j, err := s.repo.GetJob(ctx, jobID)
	if err != nil {
		return err
	}
	if j == nil {
		return shared.NewNotFound("import_job")
	}
	switch j.Status {
	case domain.StatusCompleted:
		return nil
	case domain.StatusQueued, domain.StatusValidated, domain.StatusMapped, domain.StatusProcessing:
	default:
		return shared.NewInvalidState("cannot process job in status " + string(j.Status))
	}

	j.Status = domain.StatusProcessing
	j.SuccessCount = 0
	j.FailedCount = 0
	j.SkippedCount = 0
	j.ErrorMessage = ""
	j.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateJob(ctx, j); err != nil {
		return err
	}

	sheet, err := domain.ParseFile(j.FileName, j.ContentType, j.FileBytes)
	if err != nil {
		return s.failJob(ctx, j, err.Error())
	}

	var rowErrs []domain.RowError
	success, failed, skipped := 0, 0, 0

	type pending struct {
		rowNum int
		vals   map[string]string
	}
	batch := make([]pending, 0, batchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
			for _, p := range batch {
				switch j.EntityType {
				case domain.EntityCustomers:
					ok, skip, ferr := s.applyCustomer(ctx, j, p.vals)
					if ferr != nil {
						failed++
						raw, _ := json.Marshal(p.vals)
						rowErrs = append(rowErrs, domain.RowError{
							ID: uuid.New(), JobID: j.ID, RowNumber: p.rowNum,
							Field: ferr.field, Message: ferr.msg, RawJSON: raw,
							CreatedAt: time.Now().UTC(),
						})
						continue
					}
					if skip {
						skipped++
					} else if ok {
						success++
					}
				default:
					skipped++
					raw, _ := json.Marshal(p.vals)
					rowErrs = append(rowErrs, domain.RowError{
						ID: uuid.New(), JobID: j.ID, RowNumber: p.rowNum,
						Field: "entity_type",
						Message: "import process for " + string(j.EntityType) + " is not implemented; row skipped",
						RawJSON: raw, CreatedAt: time.Now().UTC(),
					})
				}
			}
			return nil
		})
		batch = batch[:0]
		return err
	}

	for i, row := range sheet.Rows {
		vals := domain.MapRow(sheet.Headers, row, j.Mapping)
		norm, ferrs := domain.ValidateMappedRow(j.EntityType, vals)
		if len(ferrs) > 0 {
			failed++
			for field, msg := range ferrs {
				raw, _ := json.Marshal(vals)
				rowErrs = append(rowErrs, domain.RowError{
					ID: uuid.New(), JobID: j.ID, RowNumber: i + 2, Field: field,
					Message: msg, RawJSON: raw, CreatedAt: time.Now().UTC(),
				})
			}
			continue
		}
		batch = append(batch, pending{rowNum: i + 2, vals: norm})
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return s.failJob(ctx, j, err.Error())
			}
		}
	}
	if err := flush(); err != nil {
		return s.failJob(ctx, j, err.Error())
	}

	if err := s.repo.ReplaceRowErrors(ctx, j.ID, rowErrs); err != nil {
		return s.failJob(ctx, j, err.Error())
	}
	j.TotalRows = len(sheet.Rows)
	j.SuccessCount = success
	j.FailedCount = failed
	j.SkippedCount = skipped
	j.Status = domain.StatusCompleted
	j.UpdatedAt = time.Now().UTC()
	return s.repo.UpdateJob(ctx, j)
}

type fieldErr struct{ field, msg string }

func (s *Service) applyCustomer(ctx context.Context, j *domain.ImportJob, vals map[string]string) (ok, skip bool, fe *fieldErr) {
	phone := vals["phone"]
	name := strings.TrimSpace(vals["full_name"])
	if phone == "" || name == "" {
		return false, false, &fieldErr{"phone", "full_name and phone are required"}
	}
	existing, err := s.customers.FindByPhone(ctx, phone, j.BranchID)
	if err != nil {
		return false, false, &fieldErr{"phone", err.Error()}
	}

	var dob *time.Time
	if vals["date_of_birth"] != "" {
		t, err := domain.NormalizeDate(vals["date_of_birth"])
		if err != nil {
			return false, false, &fieldErr{"date_of_birth", err.Error()}
		}
		dob = t
	}

	switch j.Mode {
	case domain.ModeCreate:
		if existing != nil {
			return false, true, nil
		}
		now := time.Now().UTC()
		c := &customer.Customer{
			ID: uuid.New(), BranchID: j.BranchID, FullName: name,
			FullNameAR: strings.TrimSpace(vals["full_name_ar"]), Phone: phone,
			Email: shared.NormalizeEmail(vals["email"]),
			Nationality: strings.TrimSpace(vals["nationality"]),
			PassportNo: shared.NormalizePassport(vals["passport_no"]),
			DateOfBirth: dob, Preferences: json.RawMessage(`{}`),
			SpecialRequirements: strings.TrimSpace(vals["special_requirements"]),
			Notes: strings.TrimSpace(vals["notes"]), IsActive: true,
			CreatedBy: j.CreatedBy, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.customers.Create(ctx, c); err != nil {
			return false, false, &fieldErr{"", err.Error()}
		}
		return true, false, nil

	case domain.ModeUpdate:
		if existing == nil {
			return false, false, &fieldErr{"phone", "customer not found for update"}
		}
		patchCustomer(existing, vals, dob)
		existing.UpdatedAt = time.Now().UTC()
		if err := s.customers.Update(ctx, existing); err != nil {
			return false, false, &fieldErr{"", err.Error()}
		}
		return true, false, nil

	default:
		if existing != nil {
			patchCustomer(existing, vals, dob)
			existing.UpdatedAt = time.Now().UTC()
			if err := s.customers.Update(ctx, existing); err != nil {
				return false, false, &fieldErr{"", err.Error()}
			}
			return true, false, nil
		}
		now := time.Now().UTC()
		c := &customer.Customer{
			ID: uuid.New(), BranchID: j.BranchID, FullName: name,
			FullNameAR: strings.TrimSpace(vals["full_name_ar"]), Phone: phone,
			Email: shared.NormalizeEmail(vals["email"]),
			Nationality: strings.TrimSpace(vals["nationality"]),
			PassportNo: shared.NormalizePassport(vals["passport_no"]),
			DateOfBirth: dob, Preferences: json.RawMessage(`{}`),
			SpecialRequirements: strings.TrimSpace(vals["special_requirements"]),
			Notes: strings.TrimSpace(vals["notes"]), IsActive: true,
			CreatedBy: j.CreatedBy, CreatedAt: now, UpdatedAt: now,
		}
		if err := s.customers.Create(ctx, c); err != nil {
			return false, false, &fieldErr{"", err.Error()}
		}
		return true, false, nil
	}
}

func patchCustomer(c *customer.Customer, vals map[string]string, dob *time.Time) {
	if v := strings.TrimSpace(vals["full_name"]); v != "" {
		c.FullName = v
	}
	if v, ok := vals["full_name_ar"]; ok {
		c.FullNameAR = strings.TrimSpace(v)
	}
	if v := vals["phone"]; v != "" {
		c.Phone = v
	}
	if v, ok := vals["email"]; ok {
		c.Email = shared.NormalizeEmail(v)
	}
	if v, ok := vals["nationality"]; ok {
		c.Nationality = strings.TrimSpace(v)
	}
	if v, ok := vals["passport_no"]; ok {
		c.PassportNo = shared.NormalizePassport(v)
	}
	if dob != nil {
		c.DateOfBirth = dob
	}
	if v, ok := vals["notes"]; ok {
		c.Notes = strings.TrimSpace(v)
	}
	if v, ok := vals["special_requirements"]; ok {
		c.SpecialRequirements = strings.TrimSpace(v)
	}
}

func (s *Service) failJob(ctx context.Context, j *domain.ImportJob, msg string) error {
	j.Status = domain.StatusFailed
	j.ErrorMessage = msg
	j.UpdatedAt = time.Now().UTC()
	_ = s.repo.UpdateJob(ctx, j)
	return shared.NewValidation(msg)
}

func (s *Service) ErrorsCSV(ctx context.Context, jobID, branchID uuid.UUID) ([]byte, error) {
	if _, err := s.Get(ctx, jobID, branchID); err != nil {
		return nil, err
	}
	errs, err := s.repo.ListRowErrors(ctx, jobID)
	if err != nil {
		return nil, err
	}
	return domain.ErrorsCSV(errs)
}

func (s *Service) ListTemplates(ctx context.Context, branchID uuid.UUID, entity domain.EntityType) ([]domain.MappingTemplate, error) {
	if entity != "" && !domain.ValidEntityType(entity) {
		return nil, shared.NewValidation("invalid entity_type")
	}
	return s.repo.ListTemplates(ctx, branchID, entity)
}

func (s *Service) CreateTemplate(ctx context.Context, in TemplateInput) (*domain.MappingTemplate, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, shared.NewValidation("name is required")
	}
	if !domain.ValidEntityType(in.EntityType) {
		return nil, shared.NewValidation("invalid entity_type")
	}
	if len(in.Mapping) == 0 {
		return nil, shared.NewValidation("mapping is required")
	}
	t := &domain.MappingTemplate{
		ID: uuid.New(), BranchID: in.BranchID, Name: name,
		EntityType: in.EntityType, Mapping: in.Mapping,
		CreatedBy: in.ActorID, CreatedAt: time.Now().UTC(),
	}
	if err := s.repo.CreateTemplate(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) DeleteTemplate(ctx context.Context, id, branchID uuid.UUID) error {
	return s.repo.DeleteTemplate(ctx, id, branchID)
}

func (s *Service) ExportCSV(ctx context.Context, in ExportInput) ([]byte, string, error) {
	if !domain.ValidEntityType(in.EntityType) {
		return nil, "", shared.NewValidation("invalid entity_type")
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 10000
	}
	catalog := domain.FieldCatalog(in.EntityType)
	headers := make([]string, 0, len(catalog))
	for _, f := range catalog {
		headers = append(headers, f.Key)
	}
	var rows []domain.ExportRow
	var err error
	switch in.EntityType {
	case domain.EntityCustomers:
		rows, err = s.repo.ExportCustomers(ctx, in.BranchID, limit)
	case domain.EntityBookings:
		rows, err = s.repo.ExportBookings(ctx, in.BranchID, limit)
	case domain.EntityPayments:
		rows, err = s.repo.ExportPayments(ctx, in.BranchID, limit)
	case domain.EntityDepartures:
		rows, err = s.repo.ExportDepartures(ctx, in.BranchID, limit)
	}
	if err != nil {
		return nil, "", err
	}
	csv, err := domain.BuildCSVFromMaps(headers, rows)
	if err != nil {
		return nil, "", err
	}
	filename := string(in.EntityType) + "-export.csv"
	return csv, filename, nil
}

func (s *Service) Schemas() map[string][]domain.FieldDef {
	return map[string][]domain.FieldDef{
		string(domain.EntityCustomers):  domain.FieldCatalog(domain.EntityCustomers),
		string(domain.EntityBookings):   domain.FieldCatalog(domain.EntityBookings),
		string(domain.EntityPayments):   domain.FieldCatalog(domain.EntityPayments),
		string(domain.EntityDepartures): domain.FieldCatalog(domain.EntityDepartures),
	}
}

// ProcessByStringID is used by the worker payload.
func (s *Service) ProcessByStringID(ctx context.Context, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return shared.NewValidation("invalid import_job_id")
	}
	return s.Process(ctx, uid)
}
