package supplier

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/supplier"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, s *domain.Supplier) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO suppliers (
			id, branch_id, code, name_en, name_ar, contact_name, contact_phone, contact_email,
			terms, is_active, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.ID, s.BranchID, s.Code, s.NameEn, s.NameAr, s.ContactName, s.ContactPhone, s.ContactEmail,
		s.Terms, s.IsActive, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

func (r *Repository) Update(ctx context.Context, s *domain.Supplier) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE suppliers SET
			name_en=$2, name_ar=$3, contact_name=$4, contact_phone=$5, contact_email=$6,
			terms=$7, is_active=$8, updated_at=$9
		WHERE id=$1`,
		s.ID, s.NameEn, s.NameAr, s.ContactName, s.ContactPhone, s.ContactEmail,
		s.Terms, s.IsActive, s.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) FindByID(ctx context.Context, id uuid.UUID) (*domain.Supplier, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, code, name_en, name_ar, contact_name, contact_phone, contact_email,
			terms, is_active, created_at, updated_at
		FROM suppliers WHERE id=$1`, id)
	var s domain.Supplier
	err := row.Scan(
		&s.ID, &s.BranchID, &s.Code, &s.NameEn, &s.NameAr, &s.ContactName, &s.ContactPhone, &s.ContactEmail,
		&s.Terms, &s.IsActive, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return &s, err
}

func (r *Repository) List(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.Supplier, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	query := `
		SELECT id, branch_id, code, name_en, name_ar, contact_name, contact_phone, contact_email,
			terms, is_active, created_at, updated_at
		FROM suppliers WHERE branch_id=$1`
	if activeOnly {
		query += ` AND is_active=TRUE`
	}
	query += ` ORDER BY code`
	rows, err := q.Query(ctx, query, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Supplier
	for rows.Next() {
		var s domain.Supplier
		if err := rows.Scan(
			&s.ID, &s.BranchID, &s.Code, &s.NameEn, &s.NameAr, &s.ContactName, &s.ContactPhone, &s.ContactEmail,
			&s.Terms, &s.IsActive, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) CreateLink(ctx context.Context, l *domain.Link) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO supplier_links (
			id, supplier_id, link_type, link_id, confirmation_status, confirmation_ref, confirmed_at,
			allotment, sold, unit_cost, currency, notes, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		l.ID, l.SupplierID, string(l.LinkType), l.LinkID, string(l.ConfirmationStatus), l.ConfirmationRef, l.ConfirmedAt,
		l.Allotment, l.Sold, l.UnitCost, l.Currency, l.Notes, l.CreatedAt, l.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateLink(ctx context.Context, l *domain.Link) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE supplier_links SET
			confirmation_status=$2, confirmation_ref=$3, confirmed_at=$4, allotment=$5, sold=$6,
			unit_cost=$7, currency=$8, notes=$9, updated_at=$10
		WHERE id=$1`,
		l.ID, string(l.ConfirmationStatus), l.ConfirmationRef, l.ConfirmedAt, l.Allotment, l.Sold,
		l.UnitCost, l.Currency, l.Notes, l.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func scanLink(row pgx.Row) (*domain.Link, error) {
	var l domain.Link
	var linkType, status string
	err := row.Scan(
		&l.ID, &l.SupplierID, &linkType, &l.LinkID, &status, &l.ConfirmationRef, &l.ConfirmedAt,
		&l.Allotment, &l.Sold, &l.UnitCost, &l.Currency, &l.Notes, &l.CreatedAt, &l.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	if err != nil {
		return nil, err
	}
	l.LinkType = domain.LinkType(linkType)
	l.ConfirmationStatus = domain.ConfirmationStatus(status)
	return &l, nil
}

func (r *Repository) FindLinkByID(ctx context.Context, id uuid.UUID) (*domain.Link, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	return scanLink(q.QueryRow(ctx, `
		SELECT id, supplier_id, link_type, link_id, confirmation_status, confirmation_ref, confirmed_at,
			allotment, sold, unit_cost, currency, notes, created_at, updated_at
		FROM supplier_links WHERE id=$1`, id))
}

func (r *Repository) DeleteLink(ctx context.Context, id uuid.UUID) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `DELETE FROM supplier_links WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) ListLinksBySupplier(ctx context.Context, supplierID uuid.UUID) ([]domain.Link, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, supplier_id, link_type, link_id, confirmation_status, confirmation_ref, confirmed_at,
			allotment, sold, unit_cost, currency, notes, created_at, updated_at
		FROM supplier_links WHERE supplier_id=$1 ORDER BY created_at DESC`, supplierID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinks(rows)
}

func scanLinks(rows pgx.Rows) ([]domain.Link, error) {
	var out []domain.Link
	for rows.Next() {
		var l domain.Link
		var linkType, status string
		if err := rows.Scan(
			&l.ID, &l.SupplierID, &linkType, &l.LinkID, &status, &l.ConfirmationRef, &l.ConfirmedAt,
			&l.Allotment, &l.Sold, &l.UnitCost, &l.Currency, &l.Notes, &l.CreatedAt, &l.UpdatedAt,
		); err != nil {
			return nil, err
		}
		l.LinkType = domain.LinkType(linkType)
		l.ConfirmationStatus = domain.ConfirmationStatus(status)
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Repository) ListUnconfirmed(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.Link, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT l.id, l.supplier_id, l.link_type, l.link_id, l.confirmation_status, l.confirmation_ref, l.confirmed_at,
			l.allotment, l.sold, l.unit_cost, l.currency, l.notes, l.created_at, l.updated_at
		FROM supplier_links l
		JOIN suppliers s ON s.id = l.supplier_id
		WHERE s.branch_id=$1 AND l.confirmation_status='pending'
		ORDER BY l.created_at ASC
		LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinks(rows)
}

func (r *Repository) ListOversold(ctx context.Context, branchID uuid.UUID, limit int) ([]domain.Link, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT l.id, l.supplier_id, l.link_type, l.link_id, l.confirmation_status, l.confirmation_ref, l.confirmed_at,
			l.allotment, l.sold, l.unit_cost, l.currency, l.notes, l.created_at, l.updated_at
		FROM supplier_links l
		JOIN suppliers s ON s.id = l.supplier_id
		WHERE s.branch_id=$1 AND l.allotment > 0 AND l.sold > l.allotment
		ORDER BY (l.sold - l.allotment) DESC
		LIMIT $2`, branchID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLinks(rows)
}

func (r *Repository) CreateInvoice(ctx context.Context, inv *domain.Invoice) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO supplier_invoices (
			id, branch_id, supplier_id, invoice_number, status, currency,
			subtotal, tax_total, grand_total, issued_on, due_on, paid_at,
			notes, created_by, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		inv.ID, inv.BranchID, inv.SupplierID, inv.InvoiceNumber, string(inv.Status), inv.Currency,
		inv.Subtotal, inv.TaxTotal, inv.GrandTotal, inv.IssuedOn, inv.DueOn, inv.PaidAt,
		inv.Notes, inv.CreatedBy, inv.CreatedAt, inv.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateInvoice(ctx context.Context, inv *domain.Invoice) error {
	q := tx.QuerierFrom(ctx, r.pool)
	ct, err := q.Exec(ctx, `
		UPDATE supplier_invoices SET
			invoice_number=$2, status=$3, currency=$4, subtotal=$5, tax_total=$6, grand_total=$7,
			issued_on=$8, due_on=$9, paid_at=$10, notes=$11, updated_at=$12
		WHERE id=$1`,
		inv.ID, inv.InvoiceNumber, string(inv.Status), inv.Currency,
		inv.Subtotal, inv.TaxTotal, inv.GrandTotal, inv.IssuedOn, inv.DueOn, inv.PaidAt,
		inv.Notes, inv.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return nil
}

func (r *Repository) FindInvoiceByID(ctx context.Context, id uuid.UUID) (*domain.Invoice, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, branch_id, supplier_id, invoice_number, status, currency,
			subtotal, tax_total, grand_total, issued_on, due_on, paid_at,
			notes, created_by, created_at, updated_at
		FROM supplier_invoices WHERE id=$1`, id)
	inv, err := scanInvoice(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w", pgx.ErrNoRows)
	}
	return inv, err
}

func (r *Repository) ListInvoices(ctx context.Context, branchID uuid.UUID, supplierID *uuid.UUID, status *domain.InvoiceStatus, limit int) ([]domain.Invoice, error) {
	if limit <= 0 {
		limit = 100
	}
	q := tx.QuerierFrom(ctx, r.pool)
	statusStr := ""
	if status != nil {
		statusStr = string(*status)
	}
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, supplier_id, invoice_number, status, currency,
			subtotal, tax_total, grand_total, issued_on, due_on, paid_at,
			notes, created_by, created_at, updated_at
		FROM supplier_invoices
		WHERE branch_id=$1
			AND ($2::uuid IS NULL OR supplier_id=$2)
			AND ($3 = '' OR status=$3)
		ORDER BY created_at DESC
		LIMIT $4`, branchID, supplierID, statusStr, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *inv)
	}
	return out, rows.Err()
}

func (r *Repository) ReplaceInvoiceLines(ctx context.Context, invoiceID uuid.UUID, lines []domain.InvoiceLine) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if _, err := q.Exec(ctx, `DELETE FROM supplier_invoice_lines WHERE invoice_id=$1`, invoiceID); err != nil {
		return err
	}
	for i := range lines {
		l := &lines[i]
		if l.ID == uuid.Nil {
			l.ID = uuid.New()
		}
		l.InvoiceID = invoiceID
		if _, err := q.Exec(ctx, `
			INSERT INTO supplier_invoice_lines (
				id, invoice_id, link_id, description, quantity, unit_cost, line_total, sort_order, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			l.ID, l.InvoiceID, l.LinkID, l.Description, l.Quantity, l.UnitCost, l.LineTotal, l.SortOrder, l.CreatedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListInvoiceLines(ctx context.Context, invoiceID uuid.UUID) ([]domain.InvoiceLine, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, invoice_id, link_id, description, quantity, unit_cost, line_total, sort_order, created_at
		FROM supplier_invoice_lines
		WHERE invoice_id=$1
		ORDER BY sort_order, created_at`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InvoiceLine
	for rows.Next() {
		var l domain.InvoiceLine
		if err := rows.Scan(
			&l.ID, &l.InvoiceID, &l.LinkID, &l.Description, &l.Quantity, &l.UnitCost,
			&l.LineTotal, &l.SortOrder, &l.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func scanInvoice(row interface {
	Scan(dest ...any) error
}) (*domain.Invoice, error) {
	var inv domain.Invoice
	var status string
	err := row.Scan(
		&inv.ID, &inv.BranchID, &inv.SupplierID, &inv.InvoiceNumber, &status, &inv.Currency,
		&inv.Subtotal, &inv.TaxTotal, &inv.GrandTotal, &inv.IssuedOn, &inv.DueOn, &inv.PaidAt,
		&inv.Notes, &inv.CreatedBy, &inv.CreatedAt, &inv.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	inv.Status = domain.InvoiceStatus(status)
	return &inv, nil
}

func (r *Repository) CreateIssue(ctx context.Context, e *domain.IssueEvent) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO supplier_issue_events (
			id, branch_id, supplier_id, link_id, kind, severity, note, actor_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.ID, e.BranchID, e.SupplierID, e.LinkID, string(e.Kind), string(e.Severity),
		e.Note, e.ActorID, e.CreatedAt,
	)
	return err
}

func (r *Repository) ListIssues(ctx context.Context, supplierID uuid.UUID, limit int) ([]domain.IssueEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, supplier_id, link_id, kind, severity, note, actor_id, created_at
		FROM supplier_issue_events
		WHERE supplier_id=$1
		ORDER BY created_at DESC
		LIMIT $2`, supplierID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IssueEvent
	for rows.Next() {
		var e domain.IssueEvent
		var kind, severity string
		if err := rows.Scan(
			&e.ID, &e.BranchID, &e.SupplierID, &e.LinkID, &kind, &severity, &e.Note, &e.ActorID, &e.CreatedAt,
		); err != nil {
			return nil, err
		}
		e.Kind = domain.IssueKind(kind)
		e.Severity = domain.IssueSeverity(severity)
		out = append(out, e)
	}
	return out, rows.Err()
}
