package identity

import (
	"context"
	"strconv"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgbatch"
)

var batch = strconv.Itoa(pgbatch.Size)

// PurgeSessions deletes refresh tokens and sessions that were revoked or
// expired before cutoff. A session's tokens never outlive it, so the token
// pass leaves nothing for the cascade to hide.
func (s *SecurityRepository) PurgeSessions(ctx context.Context, cutoff time.Time) (sessions, tokens int64, err error) {
	q := s.q(ctx)
	tokens, err = pgbatch.Delete(ctx, q, `
		DELETE FROM refresh_tokens WHERE id IN (
			SELECT id FROM refresh_tokens WHERE revoked_at < $1 OR expires_at < $1 LIMIT `+batch+`)`, cutoff)
	if err != nil {
		return 0, tokens, err
	}
	sessions, err = pgbatch.Delete(ctx, q, `
		DELETE FROM auth_sessions WHERE id IN (
			SELECT id FROM auth_sessions WHERE revoked_at < $1 OR idle_expires_at < $1 LIMIT `+batch+`)`, cutoff)
	return sessions, tokens, err
}

func (s *SecurityRepository) PurgeExpiredChallenges(ctx context.Context, now time.Time) (int64, error) {
	return pgbatch.Delete(ctx, s.q(ctx), `
		DELETE FROM mfa_challenges WHERE token_hash IN (
			SELECT token_hash FROM mfa_challenges WHERE expires_at < $1 LIMIT `+batch+`)`, now)
}
