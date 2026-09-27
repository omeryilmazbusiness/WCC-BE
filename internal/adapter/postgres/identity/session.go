package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
)

// maxListedSessions bounds GET /auth/sessions; real users have a handful.
const maxListedSessions = 100

const sessionColumns = `id, user_id, created_at, absolute_expires_at, idle_expires_at, last_seen_at,
	last_ip, user_agent, auth_method, revoked_at, revoked_reason`

func (s *SecurityRepository) CreateSession(ctx context.Context, x *authsec.Session) error {
	_, err := s.q(ctx).Exec(ctx, `
		INSERT INTO auth_sessions (`+sessionColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL,'')`,
		x.ID, x.UserID, x.CreatedAt, x.AbsoluteExpiresAt, x.IdleExpiresAt, x.LastSeenAt,
		x.LastIP, x.UserAgent, x.AuthMethod)
	return err
}

func (s *SecurityRepository) FindSession(ctx context.Context, id uuid.UUID) (*authsec.Session, error) {
	x, err := scanSession(s.q(ctx).QueryRow(ctx, `SELECT `+sessionColumns+` FROM auth_sessions WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return x, err
}

func (s *SecurityRepository) TouchSession(ctx context.Context, id uuid.UUID, idleExpiresAt, seenAt time.Time, ip string) (bool, error) {
	tag, err := s.q(ctx).Exec(ctx, `
		UPDATE auth_sessions SET idle_expires_at=$2, last_seen_at=$3, last_ip=$4
		WHERE id=$1 AND revoked_at IS NULL`, id, idleExpiresAt, seenAt, ip)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *SecurityRepository) ListActiveSessions(ctx context.Context, userID uuid.UUID, now time.Time) ([]authsec.Session, error) {
	rows, err := s.q(ctx).Query(ctx, `
		SELECT `+sessionColumns+` FROM auth_sessions
		WHERE user_id=$1 AND revoked_at IS NULL AND absolute_expires_at > $2 AND idle_expires_at > $2
		ORDER BY created_at DESC
		LIMIT $3`, userID, now, maxListedSessions)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []authsec.Session{}
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *x)
	}
	return out, rows.Err()
}

func (s *SecurityRepository) RevokeSession(ctx context.Context, id uuid.UUID, reason string, now time.Time) (bool, error) {
	var n int
	err := s.q(ctx).QueryRow(ctx, `
		WITH s AS (
			UPDATE auth_sessions SET revoked_at=$2, revoked_reason=$3
			WHERE id=$1 AND revoked_at IS NULL
			RETURNING id
		), t AS (
			UPDATE refresh_tokens SET revoked_at=$2, revoked_reason=$3
			WHERE family_id=$1 AND revoked_at IS NULL
		)
		SELECT COUNT(*) FROM s`, id, now, reason).Scan(&n)
	return n == 1, err
}

func (s *SecurityRepository) RevokeUserSessions(ctx context.Context, userID, keep uuid.UUID, reason string, now time.Time) ([]uuid.UUID, error) {
	rows, err := s.q(ctx).Query(ctx, `
		WITH s AS (
			UPDATE auth_sessions SET revoked_at=$3, revoked_reason=$4
			WHERE user_id=$1 AND id<>$2 AND revoked_at IS NULL
			  AND absolute_expires_at > $3 AND idle_expires_at > $3
			RETURNING id
		), t AS (
			UPDATE refresh_tokens SET revoked_at=$3, revoked_reason=$4
			WHERE family_id IN (SELECT id FROM s) AND revoked_at IS NULL
		)
		SELECT id FROM s`, userID, keep, now, reason)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// SessionState is the single query behind every authenticated request
// (modulo the validator cache).
func (s *SecurityRepository) SessionState(ctx context.Context, sid uuid.UUID) (*authsec.SessionState, error) {
	var st authsec.SessionState
	err := s.q(ctx).QueryRow(ctx, `
		SELECT s.id, s.user_id, s.revoked_at, s.absolute_expires_at, s.idle_expires_at, u.is_active, u.token_version
		FROM auth_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.id=$1`, sid,
	).Scan(&st.SessionID, &st.UserID, &st.RevokedAt, &st.AbsoluteExpiresAt, &st.IdleExpiresAt, &st.UserActive, &st.TokenVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func scanSession(row scannable) (*authsec.Session, error) {
	var x authsec.Session
	err := row.Scan(&x.ID, &x.UserID, &x.CreatedAt, &x.AbsoluteExpiresAt, &x.IdleExpiresAt, &x.LastSeenAt,
		&x.LastIP, &x.UserAgent, &x.AuthMethod, &x.RevokedAt, &x.RevokedReason)
	if err != nil {
		return nil, err
	}
	return &x, nil
}
