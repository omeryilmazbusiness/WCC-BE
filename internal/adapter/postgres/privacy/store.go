// Package privacy is the postgres store behind KVKK export and
// anonymization. The service checks the customer is in the caller's scope
// first; everything here then follows the customer's own records.
package privacy

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/privacy"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// participantLast4 derives the display suffix, also for rows the encrypt
// backfill has not reached.
const participantLast4 = `CASE
		WHEN p.passport_last4 <> '' THEN p.passport_last4
		WHEN length(replace(p.passport_no,' ','')) > 4 THEN right(upper(replace(p.passport_no,' ','')), 4)
		ELSE ''
	END`

func (s *Store) Bookings(ctx context.Context, customerID uuid.UUID) ([]domain.Booking, error) {
	q := tx.QuerierFrom(ctx, s.pool)
	rows, err := q.Query(ctx, `
		SELECT id, departure_id, status, pax_count, total_amount, collected_amt, balance_amt, currency, created_at
		FROM bookings WHERE customer_id=$1 ORDER BY created_at, id`, customerID)
	if err != nil {
		return nil, err
	}
	var out []domain.Booking
	index := map[uuid.UUID]int{}
	for rows.Next() {
		var b domain.Booking
		if err := rows.Scan(&b.ID, &b.DepartureID, &b.Status, &b.PaxCount, &b.TotalAmount,
			&b.CollectedAmt, &b.BalanceAmt, &b.Currency, &b.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		b.Participants = []domain.Participant{}
		index[b.ID] = len(out)
		out = append(out, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	prow, err := q.Query(ctx, `
		SELECT p.id, p.booking_id, p.full_name, p.nationality, to_char(p.date_of_birth,'YYYY-MM-DD'), `+participantLast4+`
		FROM booking_participants p
		JOIN bookings b ON b.id = p.booking_id
		WHERE b.customer_id=$1
		ORDER BY p.created_at, p.id`, customerID)
	if err != nil {
		return nil, err
	}
	defer prow.Close()
	for prow.Next() {
		var p domain.Participant
		var bookingID uuid.UUID
		if err := prow.Scan(&p.ID, &bookingID, &p.FullName, &p.Nationality, &p.DateOfBirth, &p.PassportLast4); err != nil {
			return nil, err
		}
		if i, ok := index[bookingID]; ok {
			out[i].Participants = append(out[i].Participants, p)
		}
	}
	return out, prow.Err()
}

func (s *Store) PaymentTotals(ctx context.Context, customerID uuid.UUID) ([]domain.PaymentsTotal, error) {
	q := tx.QuerierFrom(ctx, s.pool)
	rows, err := q.Query(ctx, `
		SELECT p.currency, COUNT(*), COALESCE(SUM(p.amount),0)
		FROM payments p
		JOIN bookings b ON b.id = p.booking_id
		WHERE b.customer_id=$1 AND p.status <> 'rejected'
		GROUP BY p.currency ORDER BY p.currency`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.PaymentsTotal
	for rows.Next() {
		var t domain.PaymentsTotal
		if err := rows.Scan(&t.Currency, &t.Count, &t.Total); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Documents(ctx context.Context, customerID uuid.UUID) ([]domain.Document, error) {
	q := tx.QuerierFrom(ctx, s.pool)
	rows, err := q.Query(ctx, `
		SELECT id, related_type, related_id, kind, file_name, content_type, size_bytes, created_at
		FROM documents
		WHERE (related_type='customer' AND related_id=$1)
		   OR (related_type='booking' AND related_id IN (SELECT id FROM bookings WHERE customer_id=$1))
		ORDER BY created_at, id`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Document
	for rows.Next() {
		var d domain.Document
		if err := rows.Scan(&d.ID, &d.RelatedType, &d.RelatedID, &d.Kind, &d.FileName, &d.ContentType, &d.SizeBytes, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Conversations(ctx context.Context, customerID uuid.UUID) ([]domain.Conversation, error) {
	q := tx.QuerierFrom(ctx, s.pool)
	rows, err := q.Query(ctx, `
		SELECT c.id, c.channel, c.status, c.subject,
			(SELECT COUNT(*) FROM messages m WHERE m.conversation_id = c.id),
			c.last_inbound_at, c.last_outbound_at, c.created_at
		FROM conversations c
		WHERE c.customer_id=$1
		ORDER BY c.created_at, c.id`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conversation
	for rows.Next() {
		var c domain.Conversation
		if err := rows.Scan(&c.ID, &c.Channel, &c.Status, &c.Subject, &c.MessageCount,
			&c.LastInboundAt, &c.LastOutboundAt, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Companions(ctx context.Context, customerID uuid.UUID) ([]domain.CompanionRecord, error) {
	q := tx.QuerierFrom(ctx, s.pool)
	rows, err := q.Query(ctx, `
		SELECT cc.companion_id, c.full_name, cc.relation
		FROM customer_companions cc
		JOIN customers c ON c.id = cc.companion_id
		WHERE cc.customer_id=$1
		ORDER BY cc.created_at, cc.companion_id`, customerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.CompanionRecord
	for rows.Next() {
		var c domain.CompanionRecord
		if err := rows.Scan(&c.CompanionID, &c.FullName, &c.Relation); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) HasActiveBookings(ctx context.Context, customerID uuid.UUID) (bool, error) {
	q := tx.QuerierFrom(ctx, s.pool)
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM bookings WHERE customer_id=$1 AND status NOT IN ('completed','cancelled')
		)`, customerID).Scan(&ok)
	return ok, err
}

// Anonymize must run inside the caller's transaction. Payments and the audit
// trail are append-only ledgers and keep their rows; they reference the
// customer by id only.
func (s *Store) Anonymize(ctx context.Context, a domain.Anonymization) error {
	q := tx.QuerierFrom(ctx, s.pool)
	var name, hash, passport string
	if err := q.QueryRow(ctx, `
		SELECT full_name, passport_hash, upper(replace(COALESCE(passport_no,''),' ',''))
		FROM customers WHERE id=$1 FOR UPDATE`, a.CustomerID).Scan(&name, &hash, &passport); err != nil {
		return err
	}
	stmts := []struct {
		sql  string
		args []any
	}{
		// Participant rows that represent the customer on their own bookings.
		{`UPDATE booking_participants p SET full_name=$2, nationality='', date_of_birth=NULL,
				passport_no='', passport_enc='', passport_hash='', passport_last4=''
			FROM bookings b
			WHERE b.id = p.booking_id AND b.customer_id=$1
			  AND (lower(trim(p.full_name)) = lower(trim($3))
			    OR ($4 <> '' AND p.passport_hash = $4)
			    OR ($5 <> '' AND upper(replace(p.passport_no,' ','')) = $5))`,
			[]any{a.CustomerID, a.Placeholder, name, hash, passport}},
		{`UPDATE customers SET full_name=$2, full_name_ar='', phone='', email='', nationality='',
				passport_no='', passport_enc='', passport_hash='', passport_last4='', date_of_birth=NULL,
				preferences='{}'::jsonb, special_requirements='', notes='', is_active=false,
				anonymized_at=$3, updated_at=$3
			WHERE id=$1`, []any{a.CustomerID, a.Placeholder, a.At}},
		{`UPDATE leads SET full_name=$2, phone='', notes='', updated_at=$3 WHERE customer_id=$1`,
			[]any{a.CustomerID, a.Placeholder, a.At}},
		{`UPDATE channel_identities SET display_name='', phone='', email='', external_key='anonymized:' || id::text
			WHERE customer_id=$1`, []any{a.CustomerID}},
		{`UPDATE conversations SET subject='' WHERE customer_id=$1`, []any{a.CustomerID}},
		{`DELETE FROM customer_companions WHERE customer_id=$1 OR companion_id=$1`, []any{a.CustomerID}},
	}
	for _, st := range stmts {
		if _, err := q.Exec(ctx, st.sql, st.args...); err != nil {
			return err
		}
	}
	return nil
}

var _ domain.Store = (*Store)(nil)
