package inbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/inbox"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) UpsertIdentity(ctx context.Context, id *domain.ChannelIdentity) error {
	q := tx.QuerierFrom(ctx, r.pool)
	return q.QueryRow(ctx, `
		INSERT INTO channel_identities (id, branch_id, provider, external_key, display_name, phone, email, customer_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (branch_id, provider, external_key) DO UPDATE SET
			display_name = CASE WHEN EXCLUDED.display_name <> '' THEN EXCLUDED.display_name ELSE channel_identities.display_name END,
			phone = CASE WHEN EXCLUDED.phone <> '' THEN EXCLUDED.phone ELSE channel_identities.phone END,
			email = CASE WHEN EXCLUDED.email <> '' THEN EXCLUDED.email ELSE channel_identities.email END,
			customer_id = COALESCE(EXCLUDED.customer_id, channel_identities.customer_id)
		RETURNING id, created_at`,
		id.ID, id.BranchID, id.Provider, id.ExternalKey, id.DisplayName, id.Phone, id.Email, id.CustomerID, id.CreatedAt,
	).Scan(&id.ID, &id.CreatedAt)
}

func (r *Repository) FindIdentity(ctx context.Context, branchID uuid.UUID, provider domain.Channel, externalKey string) (*domain.ChannelIdentity, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var id domain.ChannelIdentity
	err := q.QueryRow(ctx, `
		SELECT id, branch_id, provider, external_key, display_name, phone, email, customer_id, created_at
		FROM channel_identities WHERE branch_id=$1 AND provider=$2 AND external_key=$3`,
		branchID, provider, externalKey,
	).Scan(&id.ID, &id.BranchID, &id.Provider, &id.ExternalKey, &id.DisplayName, &id.Phone, &id.Email, &id.CustomerID, &id.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (r *Repository) GetIdentity(ctx context.Context, id uuid.UUID) (*domain.ChannelIdentity, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var row domain.ChannelIdentity
	err := q.QueryRow(ctx, `
		SELECT id, branch_id, provider, external_key, display_name, phone, email, customer_id, created_at
		FROM channel_identities WHERE id=$1`, id,
	).Scan(&row.ID, &row.BranchID, &row.Provider, &row.ExternalKey, &row.DisplayName, &row.Phone, &row.Email, &row.CustomerID, &row.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *Repository) GetAccount(ctx context.Context, branchID uuid.UUID, provider domain.Channel) (*domain.IntegrationAccount, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var a domain.IntegrationAccount
	var cfg []byte
	err := q.QueryRow(ctx, `
		SELECT id, branch_id, provider, display_name, status, COALESCE(config_json,'{}'::jsonb), last_ok_at, last_error, updated_at
		FROM integration_accounts WHERE branch_id=$1 AND provider=$2`, branchID, provider,
	).Scan(&a.ID, &a.BranchID, &a.Provider, &a.DisplayName, &a.Status, &cfg, &a.LastOKAt, &a.LastError, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.ConfigJSON = cfg
	return &a, nil
}

func (r *Repository) ListAccounts(ctx context.Context, branchID uuid.UUID) ([]domain.IntegrationAccount, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, provider, display_name, status, COALESCE(config_json,'{}'::jsonb), last_ok_at, last_error, updated_at
		FROM integration_accounts WHERE branch_id=$1 ORDER BY provider`, branchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.IntegrationAccount
	for rows.Next() {
		var a domain.IntegrationAccount
		var cfg []byte
		if err := rows.Scan(&a.ID, &a.BranchID, &a.Provider, &a.DisplayName, &a.Status, &cfg, &a.LastOKAt, &a.LastError, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.ConfigJSON = cfg
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *Repository) UpsertAccount(ctx context.Context, a *domain.IntegrationAccount) error {
	q := tx.QuerierFrom(ctx, r.pool)
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	cfg := a.ConfigJSON
	if len(cfg) == 0 {
		cfg = []byte(`{}`)
	}
	return q.QueryRow(ctx, `
		INSERT INTO integration_accounts (id, branch_id, provider, display_name, status, config_json, last_ok_at, last_error, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6::jsonb,$7,$8,NOW(),$9)
		ON CONFLICT (branch_id, provider) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			status = EXCLUDED.status,
			config_json = EXCLUDED.config_json,
			last_ok_at = EXCLUDED.last_ok_at,
			last_error = EXCLUDED.last_error,
			updated_at = EXCLUDED.updated_at
		RETURNING id`,
		a.ID, a.BranchID, a.Provider, a.DisplayName, a.Status, string(cfg), a.LastOKAt, a.LastError, a.UpdatedAt,
	).Scan(&a.ID)
}

func (r *Repository) UpdateAccountHealth(ctx context.Context, id uuid.UUID, status, lastError string, ok bool) error {
	if id == uuid.Nil {
		return nil
	}
	q := tx.QuerierFrom(ctx, r.pool)
	if ok {
		_, err := q.Exec(ctx, `
			UPDATE integration_accounts
			SET status=$2, last_error='', last_ok_at=NOW(), updated_at=NOW()
			WHERE id=$1`, id, status)
		return err
	}
	_, err := q.Exec(ctx, `
		UPDATE integration_accounts
		SET status=$2, last_error=$3, updated_at=NOW()
		WHERE id=$1`, id, status, lastError)
	return err
}

const convSelect = `
	c.id, c.branch_id, c.channel, c.integration_account_id, c.channel_identity_id,
	c.customer_id, c.lead_id, c.owner_id, COALESCE(u.full_name,''),
	c.subject, c.status, c.sla_started_at, c.sla_due_at, c.sla_breached_at, c.sla_stopped_at,
	c.last_inbound_at, c.last_outbound_at, c.unanswered_since, c.last_message_preview,
	COALESCE(cu.full_name,''),
	TRIM(BOTH ' ·' FROM CONCAT_WS(' ·', NULLIF(ci.display_name,''), NULLIF(ci.phone,''), NULLIF(ci.email,''))),
	c.created_at, c.updated_at`

func scanConv(row pgx.Row) (*domain.Conversation, error) {
	var c domain.Conversation
	err := row.Scan(
		&c.ID, &c.BranchID, &c.Channel, &c.IntegrationAccountID, &c.ChannelIdentityID,
		&c.CustomerID, &c.LeadID, &c.OwnerID, &c.OwnerName,
		&c.Subject, &c.Status, &c.SLAStartedAt, &c.SLADueAt, &c.SLABreachedAt, &c.SLAStoppedAt,
		&c.LastInboundAt, &c.LastOutboundAt, &c.UnansweredSince, &c.LastMessagePreview,
		&c.CustomerName, &c.IdentityLabel, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) CreateConversation(ctx context.Context, c *domain.Conversation) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO conversations (
			id, branch_id, channel, integration_account_id, channel_identity_id,
			customer_id, lead_id, owner_id, subject, status,
			sla_started_at, sla_due_at, sla_breached_at, sla_stopped_at,
			last_inbound_at, last_outbound_at, unanswered_since, last_message_preview,
			created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
		)`,
		c.ID, c.BranchID, c.Channel, c.IntegrationAccountID, c.ChannelIdentityID,
		c.CustomerID, c.LeadID, c.OwnerID, c.Subject, c.Status,
		c.SLAStartedAt, c.SLADueAt, c.SLABreachedAt, c.SLAStoppedAt,
		c.LastInboundAt, c.LastOutboundAt, c.UnansweredSince, c.LastMessagePreview,
		c.CreatedAt, c.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateConversation(ctx context.Context, c *domain.Conversation) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE conversations SET
			customer_id=$2, lead_id=$3, owner_id=$4, subject=$5, status=$6,
			sla_started_at=$7, sla_due_at=$8, sla_breached_at=$9, sla_stopped_at=$10,
			last_inbound_at=$11, last_outbound_at=$12, unanswered_since=$13,
			last_message_preview=$14, updated_at=$15
		WHERE id=$1`,
		c.ID, c.CustomerID, c.LeadID, c.OwnerID, c.Subject, c.Status,
		c.SLAStartedAt, c.SLADueAt, c.SLABreachedAt, c.SLAStoppedAt,
		c.LastInboundAt, c.LastOutboundAt, c.UnansweredSince, c.LastMessagePreview, c.UpdatedAt,
	)
	return err
}

func (r *Repository) GetConversation(ctx context.Context, id uuid.UUID) (*domain.Conversation, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT `+convSelect+`
		FROM conversations c
		LEFT JOIN users u ON u.id = c.owner_id
		LEFT JOIN customers cu ON cu.id = c.customer_id
		LEFT JOIN channel_identities ci ON ci.id = c.channel_identity_id
		WHERE c.id=$1`, id)
	c, err := scanConv(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (r *Repository) FindOpenByIdentity(ctx context.Context, identityID uuid.UUID) (*domain.Conversation, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT `+convSelect+`
		FROM conversations c
		LEFT JOIN users u ON u.id = c.owner_id
		LEFT JOIN customers cu ON cu.id = c.customer_id
		LEFT JOIN channel_identities ci ON ci.id = c.channel_identity_id
		WHERE c.channel_identity_id=$1 AND c.status='open'
		ORDER BY c.updated_at DESC LIMIT 1`, identityID)
	c, err := scanConv(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return c, err
}

func (r *Repository) ListConversations(ctx context.Context, f domain.ListFilter) ([]domain.Conversation, int64, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.BranchID != nil {
		add("c.branch_id=$%d", *f.BranchID)
	}
	if f.Status != "" {
		add("c.status=$%d", f.Status)
	} else {
		where = append(where, "c.status='open'")
	}
	if f.Channel != "" {
		add("c.channel=$%d", f.Channel)
	}
	if f.UnassignedOnly {
		where = append(where, "c.owner_id IS NULL")
	}
	if f.MineOnly && f.OwnerID != nil {
		add("c.owner_id=$%d", *f.OwnerID)
	} else if f.OwnerID != nil && !f.MineOnly {
		add("c.owner_id=$%d", *f.OwnerID)
	}
	if f.SLABreached != nil && *f.SLABreached {
		where = append(where, "c.sla_breached_at IS NOT NULL")
	}
	if f.UnansweredMin != nil {
		cutoff := time.Now().UTC().Add(-*f.UnansweredMin)
		add("c.unanswered_since IS NOT NULL AND c.unanswered_since <= $%d", cutoff)
	}
	if qstr := strings.TrimSpace(f.Query); qstr != "" {
		args = append(args, qstr)
		n := len(args)
		where = append(where, fmt.Sprintf(
			"(c.subject ILIKE '%%' || $%d || '%%' OR c.last_message_preview ILIKE '%%' || $%d || '%%' OR COALESCE(ci.display_name,'') ILIKE '%%' || $%d || '%%')",
			n, n, n,
		))
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	wsql := strings.Join(where, " AND ")
	var total int64
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM conversations c
		LEFT JOIN channel_identities ci ON ci.id = c.channel_identity_id
		WHERE `+wsql, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args2 := append(append([]any{}, args...), limit, offset)
	rows, err := q.Query(ctx, `
		SELECT `+convSelect+`
		FROM conversations c
		LEFT JOIN users u ON u.id = c.owner_id
		LEFT JOIN customers cu ON cu.id = c.customer_id
		LEFT JOIN channel_identities ci ON ci.id = c.channel_identity_id
		WHERE `+wsql+`
		ORDER BY c.sla_breached_at DESC NULLS LAST, c.unanswered_since ASC NULLS LAST, c.updated_at DESC
		LIMIT $`+fmt.Sprint(len(args)+1)+` OFFSET $`+fmt.Sprint(len(args)+2), args2...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.Conversation
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

func (r *Repository) InsertMessage(ctx context.Context, m *domain.Message) error {
	q := tx.QuerierFrom(ctx, r.pool)
	var eventID, msgID *string
	if m.ProviderEventID != "" {
		eventID = &m.ProviderEventID
	}
	if m.ProviderMessageID != "" {
		msgID = &m.ProviderMessageID
	}
	_, err := q.Exec(ctx, `
		INSERT INTO messages (
			id, conversation_id, branch_id, direction, body, content_type, provider,
			provider_message_id, provider_event_id, status, document_id, author_user_id,
			error_message, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		m.ID, m.ConversationID, m.BranchID, m.Direction, m.Body, m.ContentType, m.Provider,
		msgID, eventID, m.Status, m.DocumentID, m.AuthorUserID, m.ErrorMessage, m.CreatedAt,
	)
	return err
}

func (r *Repository) FindMessageByEvent(ctx context.Context, provider domain.Channel, eventID string) (*domain.Message, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var m domain.Message
	var pMsgID, pEvtID *string
	err := q.QueryRow(ctx, `
		SELECT id, conversation_id, branch_id, direction, body, content_type, provider,
			provider_message_id, provider_event_id, status, document_id, author_user_id,
			error_message, created_at
		FROM messages WHERE provider=$1 AND provider_event_id=$2`, provider, eventID,
	).Scan(&m.ID, &m.ConversationID, &m.BranchID, &m.Direction, &m.Body, &m.ContentType, &m.Provider,
		&pMsgID, &pEvtID, &m.Status, &m.DocumentID, &m.AuthorUserID, &m.ErrorMessage, &m.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pMsgID != nil {
		m.ProviderMessageID = *pMsgID
	}
	if pEvtID != nil {
		m.ProviderEventID = *pEvtID
	}
	return &m, nil
}

func (r *Repository) ListMessages(ctx context.Context, conversationID uuid.UUID, limit int) ([]domain.Message, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT m.id, m.conversation_id, m.branch_id, m.direction, m.body, m.content_type, m.provider,
			m.provider_message_id, m.provider_event_id, m.status, m.document_id, m.author_user_id,
			COALESCE(u.full_name,''), m.error_message, m.created_at
		FROM messages m
		LEFT JOIN users u ON u.id = m.author_user_id
		WHERE m.conversation_id=$1
		ORDER BY m.created_at ASC
		LIMIT $2`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Message
	for rows.Next() {
		var m domain.Message
		var pMsgID, pEvtID *string
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.BranchID, &m.Direction, &m.Body, &m.ContentType, &m.Provider,
			&pMsgID, &pEvtID, &m.Status, &m.DocumentID, &m.AuthorUserID, &m.AuthorName, &m.ErrorMessage, &m.CreatedAt); err != nil {
			return nil, err
		}
		if pMsgID != nil {
			m.ProviderMessageID = *pMsgID
		}
		if pEvtID != nil {
			m.ProviderEventID = *pEvtID
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repository) FirstResponseSeconds(ctx context.Context, branchID uuid.UUID, channel domain.Channel) (int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var secs int
	err := q.QueryRow(ctx, `
		SELECT first_response_seconds FROM sla_policies
		WHERE branch_id=$1 AND (channel=$2 OR channel='*')
		ORDER BY CASE WHEN channel=$2 THEN 0 ELSE 1 END
		LIMIT 1`, branchID, channel).Scan(&secs)
	if err == pgx.ErrNoRows {
		return int(domain.DefaultFirstResponse.Seconds()), nil
	}
	return secs, err
}

func (r *Repository) ListDueForSLABreach(ctx context.Context, now time.Time, limit int) ([]domain.Conversation, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT `+convSelect+`
		FROM conversations c
		LEFT JOIN users u ON u.id = c.owner_id
		LEFT JOIN customers cu ON cu.id = c.customer_id
		LEFT JOIN channel_identities ci ON ci.id = c.channel_identity_id
		WHERE c.status='open' AND c.sla_stopped_at IS NULL AND c.sla_breached_at IS NULL
		  AND c.sla_due_at IS NOT NULL AND c.sla_due_at < $1
		ORDER BY c.sla_due_at ASC
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conversation
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *Repository) MarkSLABreached(ctx context.Context, id uuid.UUID, at time.Time) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE conversations SET sla_breached_at=$2, updated_at=$2 WHERE id=$1 AND sla_breached_at IS NULL`,
		id, at)
	return err
}
