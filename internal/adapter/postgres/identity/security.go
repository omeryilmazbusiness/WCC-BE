package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	appauth "github.com/wodi-crm/wodi-crm-be/internal/app/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// SecurityRepository persists MFA, lockout, challenge and refresh-token state.
type SecurityRepository struct {
	repo *Repository
}

func NewSecurityRepository(r *Repository) *SecurityRepository {
	return &SecurityRepository{repo: r}
}

func (s *SecurityRepository) q(ctx context.Context) tx.Querier {
	return tx.QuerierFrom(ctx, s.repo.pool)
}

func (s *SecurityRepository) SecurityState(ctx context.Context, userID uuid.UUID) (*appauth.SecurityState, error) {
	var st appauth.SecurityState
	err := s.q(ctx).QueryRow(ctx, `
		SELECT COALESCE(mfa_enabled,false), COALESCE(mfa_secret_enc,''), mfa_pending_secret_enc, mfa_last_step,
		       failed_login_count, lockout_count, locked_until
		FROM users WHERE id=$1`, userID,
	).Scan(&st.MFAEnabled, &st.MFASecretEnc, &st.MFAPendingEnc, &st.MFALastStep,
		&st.FailedLogins, &st.Lockouts, &st.LockedUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *SecurityRepository) RegisterLoginFailure(ctx context.Context, userID uuid.UUID, windowStart, now time.Time) (int, int, error) {
	var failures, lockouts int
	err := s.q(ctx).QueryRow(ctx, `
		UPDATE users SET
			failed_login_count = CASE
				WHEN last_failed_login_at IS NULL OR last_failed_login_at < $2 THEN 1
				ELSE failed_login_count + 1 END,
			last_failed_login_at = $3
		WHERE id=$1
		RETURNING failed_login_count, lockout_count`, userID, windowStart, now,
	).Scan(&failures, &lockouts)
	return failures, lockouts, err
}

func (s *SecurityRepository) LockUser(ctx context.Context, userID uuid.UUID, until time.Time) error {
	_, err := s.q(ctx).Exec(ctx, `
		UPDATE users SET locked_until=$2, lockout_count=lockout_count+1, failed_login_count=0
		WHERE id=$1`, userID, until)
	return err
}

func (s *SecurityRepository) ClearLoginFailures(ctx context.Context, userID uuid.UUID) error {
	_, err := s.q(ctx).Exec(ctx, `
		UPDATE users SET failed_login_count=0, lockout_count=0, locked_until=NULL, last_failed_login_at=NULL
		WHERE id=$1 AND (failed_login_count<>0 OR lockout_count<>0 OR locked_until IS NOT NULL)`, userID)
	return err
}

func (s *SecurityRepository) UnlockUser(ctx context.Context, userID uuid.UUID) error {
	_, err := s.q(ctx).Exec(ctx, `
		UPDATE users SET failed_login_count=0, lockout_count=0, locked_until=NULL, last_failed_login_at=NULL
		WHERE id=$1`, userID)
	return err
}

func (s *SecurityRepository) SetPendingMFASecret(ctx context.Context, userID uuid.UUID, enc string) error {
	_, err := s.q(ctx).Exec(ctx, `UPDATE users SET mfa_pending_secret_enc=$2 WHERE id=$1`, userID, enc)
	return err
}

func (s *SecurityRepository) ActivateMFA(ctx context.Context, userID uuid.UUID, secretEnc string, step int64, now time.Time) error {
	_, err := s.q(ctx).Exec(ctx, `
		UPDATE users SET mfa_enabled=TRUE, mfa_secret_enc=$2, mfa_pending_secret_enc='',
		       mfa_last_step=$3, mfa_enrolled_at=$4, updated_at=$4
		WHERE id=$1`, userID, secretEnc, step, now)
	return err
}

func (s *SecurityRepository) DeactivateMFA(ctx context.Context, userID uuid.UUID) error {
	q := s.q(ctx)
	if _, err := q.Exec(ctx, `
		UPDATE users SET mfa_enabled=FALSE, mfa_secret_enc='', mfa_pending_secret_enc='',
		       mfa_last_step=0, mfa_enrolled_at=NULL, updated_at=NOW()
		WHERE id=$1`, userID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id=$1`, userID)
	return err
}

func (s *SecurityRepository) AdvanceMFAStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error) {
	tag, err := s.q(ctx).Exec(ctx, `UPDATE users SET mfa_last_step=$2 WHERE id=$1 AND mfa_last_step < $2`, userID, step)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *SecurityRepository) ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes []string) error {
	q := s.q(ctx)
	if _, err := q.Exec(ctx, `DELETE FROM mfa_recovery_codes WHERE user_id=$1`, userID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO mfa_recovery_codes (user_id, code_hash)
		SELECT $1, h FROM unnest($2::text[]) AS h`, userID, hashes)
	return err
}

func (s *SecurityRepository) ConsumeRecoveryCode(ctx context.Context, userID uuid.UUID, hash string, now time.Time) (bool, error) {
	tag, err := s.q(ctx).Exec(ctx, `
		UPDATE mfa_recovery_codes SET used_at=$3
		WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL`, userID, hash, now)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *SecurityRepository) CountRecoveryCodes(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.q(ctx).QueryRow(ctx, `
		SELECT COUNT(*) FROM mfa_recovery_codes WHERE user_id=$1 AND used_at IS NULL`, userID).Scan(&n)
	return n, err
}

func (s *SecurityRepository) CreateChallenge(ctx context.Context, c authsec.Challenge) error {
	q := s.q(ctx)
	if _, err := q.Exec(ctx, `DELETE FROM mfa_challenges WHERE user_id=$1 AND expires_at < NOW()`, c.UserID); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `
		INSERT INTO mfa_challenges (token_hash, user_id, purpose, attempts, expires_at, created_ip)
		VALUES ($1,$2,$3,0,$4,$5)`, c.TokenHash, c.UserID, c.Purpose, c.ExpiresAt, c.CreatedIP)
	return err
}

func (s *SecurityRepository) FindChallenge(ctx context.Context, tokenHash, purpose string, now time.Time) (*authsec.Challenge, error) {
	return scanChallenge(s.q(ctx).QueryRow(ctx, `
		SELECT token_hash, user_id, purpose, attempts, expires_at, created_ip
		FROM mfa_challenges WHERE token_hash=$1 AND purpose=$2 AND expires_at > $3`, tokenHash, purpose, now))
}

func (s *SecurityRepository) AttemptChallenge(ctx context.Context, tokenHash, purpose string, maxAttempts int, now time.Time) (*authsec.Challenge, error) {
	return scanChallenge(s.q(ctx).QueryRow(ctx, `
		UPDATE mfa_challenges SET attempts = attempts + 1
		WHERE token_hash=$1 AND purpose=$2 AND expires_at > $3 AND attempts < $4
		RETURNING token_hash, user_id, purpose, attempts, expires_at, created_ip`,
		tokenHash, purpose, now, maxAttempts))
}

func (s *SecurityRepository) DeleteChallenge(ctx context.Context, tokenHash string) error {
	_, err := s.q(ctx).Exec(ctx, `DELETE FROM mfa_challenges WHERE token_hash=$1`, tokenHash)
	return err
}

func scanChallenge(row pgx.Row) (*authsec.Challenge, error) {
	var c authsec.Challenge
	err := row.Scan(&c.TokenHash, &c.UserID, &c.Purpose, &c.Attempts, &c.ExpiresAt, &c.CreatedIP)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *SecurityRepository) CreateRefreshToken(ctx context.Context, t *authsec.RefreshToken) error {
	_, err := s.q(ctx).Exec(ctx, `
		INSERT INTO refresh_tokens (id, user_id, family_id, token_hash, expires_at, created_ip, user_agent, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		t.ID, t.UserID, t.FamilyID, t.TokenHash, t.ExpiresAt, t.CreatedIP, t.UserAgent, t.CreatedAt)
	return err
}

func (s *SecurityRepository) FindRefreshToken(ctx context.Context, tokenHash string) (*authsec.RefreshToken, error) {
	var t authsec.RefreshToken
	err := s.q(ctx).QueryRow(ctx, `
		SELECT id, user_id, family_id, token_hash, expires_at, revoked_at, revoked_reason, replaced_by,
		       created_ip, user_agent, created_at
		FROM refresh_tokens WHERE token_hash=$1`, tokenHash,
	).Scan(&t.ID, &t.UserID, &t.FamilyID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.RevokedReason,
		&t.ReplacedBy, &t.CreatedIP, &t.UserAgent, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *SecurityRepository) MarkRefreshRotated(ctx context.Context, id, replacedBy uuid.UUID, now time.Time) (bool, error) {
	tag, err := s.q(ctx).Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at=$3, revoked_reason=$4, replaced_by=$2
		WHERE id=$1 AND revoked_at IS NULL`, id, replacedBy, now, authsec.RevokeRotated)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

var (
	_ appauth.LockoutStore   = (*SecurityRepository)(nil)
	_ appauth.MFAStore       = (*SecurityRepository)(nil)
	_ appauth.ChallengeStore = (*SecurityRepository)(nil)
	_ appauth.RefreshStore   = (*SecurityRepository)(nil)
	_ appauth.SessionStore   = (*SecurityRepository)(nil)
)
