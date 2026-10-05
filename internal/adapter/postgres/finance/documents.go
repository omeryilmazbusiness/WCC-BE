package finance

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
)

// ---- BSP ----

func (r *Repository) SystemTickets(ctx context.Context, branchID uuid.UUID, from, to time.Time, currency string) ([]domain.SystemTicket, error) {
	clause, args, err := pgscope.Clause(ctx, branchOnly("b.branch_id"), []any{branchID, from, to, currency})
	if err != nil {
		return nil, err
	}
	rows, err := r.q(ctx).Query(ctx, `
		SELECT b.id, upper(b.pnr), b.cost_amt, COALESCE(b.currency,'SAR') FROM bookings b
		WHERE b.branch_id=$1 AND b.service_type='flight' AND b.pnr <> '' AND b.status IN `+revenueBookings+`
			AND b.created_at >= $2 AND b.created_at < $3 AND COALESCE(b.currency,'SAR')=$4`+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SystemTicket
	for rows.Next() {
		var t domain.SystemTicket
		if err := rows.Scan(&t.BookingID, &t.PNR, &t.Amount, &t.Currency); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) InsertStatement(ctx context.Context, st *domain.BSPStatement, lines []domain.BSPLine) error {
	if err := pgscope.EnsureBranch(ctx, st.BranchID); err != nil {
		return err
	}
	q := r.q(ctx)
	if _, err := q.Exec(ctx, `INSERT INTO bsp_statements (id, branch_id, label, period_start, period_end, currency, total,
		system_total, line_count, matched, mismatched, missing_system, missing_bsp, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		st.ID, st.BranchID, st.Label, st.PeriodStart, st.PeriodEnd, st.Currency, st.Total, st.SystemTotal, st.LineCount,
		st.Matched, st.Mismatched, st.MissingSystem, st.MissingBSP, st.CreatedBy, st.CreatedAt); err != nil {
		return err
	}
	const cols, chunk = 12, 500
	for start := 0; start < len(lines); start += chunk {
		end := min(start+chunk, len(lines))
		var sb strings.Builder
		args := make([]any, 0, (end-start)*cols)
		for i := start; i < end; i++ {
			l := lines[i]
			if i > start {
				sb.WriteByte(',')
			}
			sb.WriteByte('(')
			for c := 1; c <= cols; c++ {
				if c > 1 {
					sb.WriteByte(',')
				}
				sb.WriteString(placeholder(len(args) + c))
			}
			sb.WriteByte(')')
			args = append(args, l.ID, st.ID, l.DocumentNo, l.PNR, l.Type, l.Passenger, l.IssuedOn, l.Amount, l.BookingID,
				l.SystemAmount, l.Status, i)
		}
		if _, err := q.Exec(ctx, `INSERT INTO bsp_lines (id, statement_id, document_no, pnr, doc_type, passenger, issued_on,
			amount, booking_id, system_amount, status, sort_order) VALUES `+sb.String(), args...); err != nil {
			return err
		}
	}
	return nil
}

const statementColumns = `s.id, s.branch_id, s.label, s.period_start, s.period_end, s.currency, s.total, s.system_total,
	s.line_count, s.matched, s.mismatched, s.missing_system, s.missing_bsp, s.created_by, s.created_at`

func scanStatement(row pgx.Row) (*domain.BSPStatement, error) {
	var s domain.BSPStatement
	err := row.Scan(&s.ID, &s.BranchID, &s.Label, &s.PeriodStart, &s.PeriodEnd, &s.Currency, &s.Total, &s.SystemTotal,
		&s.LineCount, &s.Matched, &s.Mismatched, &s.MissingSystem, &s.MissingBSP, &s.CreatedBy, &s.CreatedAt)
	return &s, err
}

func (r *Repository) ListStatements(ctx context.Context, branchID *uuid.UUID, limit int) ([]domain.BSPStatement, error) {
	w := newWhere(branchID, "s.branch_id")
	if err := w.scope(ctx, branchOnly("s.branch_id")); err != nil {
		return nil, err
	}
	w.args = append(w.args, limit)
	rows, err := r.q(ctx).Query(ctx, `SELECT `+statementColumns+` FROM bsp_statements s WHERE `+w.sql()+`
		ORDER BY s.period_end DESC, s.created_at DESC LIMIT `+placeholder(len(w.args)), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.BSPStatement{}
	for rows.Next() {
		s, err := scanStatement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *Repository) FindStatement(ctx context.Context, id uuid.UUID) (*domain.BSPStatement, []domain.BSPLine, error) {
	clause, args, err := pgscope.Clause(ctx, branchOnly("s.branch_id"), []any{id})
	if err != nil {
		return nil, nil, err
	}
	st, err := scanStatement(r.q(ctx).QueryRow(ctx, `SELECT `+statementColumns+` FROM bsp_statements s WHERE s.id=$1`+clause, args...))
	if err != nil {
		return nil, nil, notFound(err, "bsp statement")
	}
	rows, err := r.q(ctx).Query(ctx, `SELECT id, statement_id, document_no, pnr, doc_type, passenger, issued_on, amount,
		booking_id, system_amount, status FROM bsp_lines WHERE statement_id=$1 ORDER BY
		CASE status WHEN 'amount_mismatch' THEN 0 WHEN 'missing_in_system' THEN 1 WHEN 'missing_in_bsp' THEN 2 ELSE 3 END, sort_order`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	lines := []domain.BSPLine{}
	for rows.Next() {
		var l domain.BSPLine
		if err := rows.Scan(&l.ID, &l.StatementID, &l.DocumentNo, &l.PNR, &l.Type, &l.Passenger, &l.IssuedOn, &l.Amount,
			&l.BookingID, &l.SystemAmount, &l.Status); err != nil {
			return nil, nil, err
		}
		lines = append(lines, l)
	}
	return st, lines, rows.Err()
}

// ---- letters ----

const letterColumns = `l.id, l.branch_id, l.party_type, l.party_id, l.party_name, l.period_end, l.balance, l.currency,
	l.email, l.token_hash, l.status, l.response_note, l.responded_by, l.responded_at, l.expires_at, l.created_by, l.created_at`

func scanLetter(row pgx.Row) (*domain.Letter, error) {
	var l domain.Letter
	err := row.Scan(&l.ID, &l.BranchID, &l.PartyType, &l.PartyID, &l.PartyName, &l.PeriodEnd, &l.Balance, &l.Currency,
		&l.Email, &l.TokenHash, &l.Status, &l.ResponseNote, &l.RespondedBy, &l.RespondedAt, &l.ExpiresAt, &l.CreatedBy, &l.CreatedAt)
	return &l, err
}

func (r *Repository) InsertLetter(ctx context.Context, l *domain.Letter) error {
	if err := pgscope.EnsureBranch(ctx, l.BranchID); err != nil {
		return err
	}
	_, err := r.q(ctx).Exec(ctx, `INSERT INTO reconciliation_letters (id, branch_id, party_type, party_id, party_name,
		period_end, balance, currency, email, token_hash, status, expires_at, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		l.ID, l.BranchID, l.PartyType, l.PartyID, l.PartyName, l.PeriodEnd, l.Balance, l.Currency, l.Email, l.TokenHash,
		l.Status, l.ExpiresAt, l.CreatedBy, l.CreatedAt)
	return err
}

func (r *Repository) ListLetters(ctx context.Context, branchID *uuid.UUID, limit int) ([]domain.Letter, error) {
	w := newWhere(branchID, "l.branch_id")
	if err := w.scope(ctx, branchOnly("l.branch_id")); err != nil {
		return nil, err
	}
	w.args = append(w.args, limit)
	rows, err := r.q(ctx).Query(ctx, `SELECT `+letterColumns+` FROM reconciliation_letters l WHERE `+w.sql()+`
		ORDER BY l.created_at DESC LIMIT `+placeholder(len(w.args)), w.args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Letter{}
	for rows.Next() {
		l, err := scanLetter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

func (r *Repository) FindLetterByToken(ctx context.Context, tokenHash string) (*domain.Letter, error) {
	l, err := scanLetter(r.q(ctx).QueryRow(ctx, `SELECT `+letterColumns+` FROM reconciliation_letters l WHERE l.token_hash=$1`, tokenHash))
	if err != nil {
		return nil, notFound(err, "confirmation request")
	}
	return l, nil
}

func (r *Repository) SaveLetterResponse(ctx context.Context, l *domain.Letter) error {
	ct, err := r.q(ctx).Exec(ctx, `UPDATE reconciliation_letters SET status=$2, response_note=$3, responded_by=$4,
		responded_at=$5 WHERE id=$1 AND status='sent'`, l.ID, l.Status, l.ResponseNote, l.RespondedBy, l.RespondedAt)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return conflict("this confirmation request was already answered")
	}
	return nil
}
