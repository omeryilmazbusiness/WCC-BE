package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/totp"
)

const (
	methodTOTP     = "totp"
	methodRecovery = "recovery_code"
)

var errBadSecondFactor = errors.New("bad second factor")

// MFAEnrollment is shown once to the user to configure an authenticator app.
type MFAEnrollment struct {
	Secret     string `json:"secret"`
	OTPAuthURL string `json:"otpauth_url"`
}

// MFACodeInput is the authenticated-user MFA management request.
type MFACodeInput struct {
	UserID    uuid.UUID
	Code      string
	Password  string
	IP        string
	UserAgent string
}

// MFASetupInput is the enrollment-token flow for forced roles at login.
type MFASetupInput struct {
	EnrollmentToken string
	Code            string
	IP              string
	UserAgent       string
}

func secretAAD(userID uuid.UUID) string  { return "users.mfa_secret:" + userID.String() }
func pendingAAD(userID uuid.UUID) string { return "users.mfa_pending_secret:" + userID.String() }

// Enroll starts (or restarts) enrollment for an authenticated user.
func (s *Service) Enroll(ctx context.Context, in MFACodeInput) (*MFAEnrollment, error) {
	user, st, err := s.loadUser(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	if st.MFAEnabled && st.MFASecretEnc != "" {
		return nil, shared.NewConflict("mfa is already enabled")
	}
	return s.startEnrollment(ctx, user, requestMeta{IP: in.IP, UserAgent: in.UserAgent})
}

// ConfirmEnrollment enables MFA and returns one-time recovery codes.
func (s *Service) ConfirmEnrollment(ctx context.Context, in MFACodeInput) ([]string, error) {
	user, st, err := s.loadUser(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	if st.MFAEnabled && st.MFASecretEnc != "" {
		return nil, shared.NewConflict("mfa is already enabled")
	}
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	codes, err := s.confirmEnrollment(ctx, user, st, in.Code, meta)
	if errors.Is(err, errBadSecondFactor) {
		return nil, shared.NewValidation("invalid mfa code")
	}
	return codes, err
}

// DisableMFA requires the password and a current TOTP or recovery code.
func (s *Service) DisableMFA(ctx context.Context, in MFACodeInput) error {
	user, st, err := s.loadUser(ctx, in.UserID)
	if err != nil {
		return err
	}
	if !st.MFAEnabled || st.MFASecretEnc == "" {
		return shared.NewInvalidState("mfa is not enabled")
	}
	if s.opts.MFA.Required(user.Role) {
		return shared.NewForbidden("mfa is mandatory for this role")
	}
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	fail := shared.NewValidation("invalid password or mfa code")
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)) != nil {
		s.record(ctx, user.ID, user, "auth.mfa_disable_failed", meta, map[string]any{"reason": "password"})
		return s.registerFailure(ctx, user, user.Email, meta, "mfa_disable_password", fail)
	}
	method, err := s.checkSecondFactor(ctx, user.ID, st, in.Code, true)
	if errors.Is(err, errBadSecondFactor) {
		s.record(ctx, user.ID, user, "auth.mfa_disable_failed", meta, map[string]any{"reason": "code"})
		return s.registerFailure(ctx, user, user.Email, meta, "mfa_disable_code", fail)
	}
	if err != nil {
		return err
	}
	if err := s.mfa.DeactivateMFA(ctx, user.ID); err != nil {
		return err
	}
	s.record(ctx, user.ID, user, "auth.mfa_disabled", meta, map[string]any{"method": method})
	return nil
}

// RegenerateRecoveryCodes replaces all recovery codes after a TOTP check.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, in MFACodeInput) ([]string, error) {
	user, st, err := s.loadUser(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	if !st.MFAEnabled || st.MFASecretEnc == "" {
		return nil, shared.NewInvalidState("mfa is not enabled")
	}
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	if _, err := s.checkSecondFactor(ctx, user.ID, st, in.Code, false); err != nil {
		if errors.Is(err, errBadSecondFactor) {
			s.record(ctx, user.ID, user, "auth.mfa_failed", meta, map[string]any{"context": "recovery_regenerate"})
			return nil, s.registerFailure(ctx, user, user.Email, meta, "mfa", shared.NewValidation("invalid mfa code"))
		}
		return nil, err
	}
	codes, hashes, err := authsec.NewRecoveryCodes(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := s.mfa.ReplaceRecoveryCodes(ctx, user.ID, hashes); err != nil {
		return nil, err
	}
	s.record(ctx, user.ID, user, "auth.mfa_recovery_regenerated", meta, nil)
	return codes, nil
}

// SetupStart begins enrollment with the enrollment token from login.
func (s *Service) SetupStart(ctx context.Context, in MFASetupInput) (*MFAEnrollment, error) {
	ch, err := s.challenges.FindChallenge(ctx, authsec.HashToken(in.EnrollmentToken), authsec.ChallengeEnroll, s.now())
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, shared.NewUnauthorized("invalid or expired enrollment token")
	}
	user, st, err := s.loadUser(ctx, ch.UserID)
	if err != nil || !user.IsActive {
		return nil, shared.NewUnauthorized("invalid or expired enrollment token")
	}
	if st.MFAEnabled && st.MFASecretEnc != "" {
		return nil, shared.NewConflict("mfa is already enabled")
	}
	return s.startEnrollment(ctx, user, requestMeta{IP: in.IP, UserAgent: in.UserAgent})
}

// SetupConfirm enables MFA and completes the login that required it.
func (s *Service) SetupConfirm(ctx context.Context, in MFASetupInput) (*LoginResult, error) {
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	if err := s.checkIPThrottle(ctx, meta.IP); err != nil {
		return nil, err
	}
	hash := authsec.HashToken(in.EnrollmentToken)
	ch, err := s.challenges.AttemptChallenge(ctx, hash, authsec.ChallengeEnroll, maxChallengeAttempts, s.now())
	if err != nil {
		return nil, err
	}
	invalid := shared.NewUnauthorized("invalid or expired enrollment token")
	if ch == nil {
		return nil, invalid
	}
	user, st, err := s.loadUser(ctx, ch.UserID)
	if err != nil || !user.IsActive {
		_ = s.challenges.DeleteChallenge(ctx, hash)
		return nil, invalid
	}
	if wait := authsec.RetryAfter(st.LockedUntil, s.now()); wait > 0 {
		_ = s.challenges.DeleteChallenge(ctx, hash)
		return nil, lockedError(wait)
	}
	codes, err := s.confirmEnrollment(ctx, user, st, in.Code, meta)
	if errors.Is(err, errBadSecondFactor) {
		if ch.Attempts >= maxChallengeAttempts {
			_ = s.challenges.DeleteChallenge(ctx, hash)
		}
		return nil, s.registerFailure(ctx, user, user.Email, meta, "mfa_setup", errInvalidMFACode)
	}
	if err != nil {
		return nil, err
	}
	_ = s.challenges.DeleteChallenge(ctx, hash)
	user.MFAEnabled = true
	pair, err := s.completeLogin(ctx, user, user.Email, meta, "mfa_setup")
	if err != nil {
		return nil, err
	}
	return &LoginResult{Tokens: pair, User: publicUser(user, nil), RecoveryCodes: codes}, nil
}

func (s *Service) startEnrollment(ctx context.Context, user *identity.User, meta requestMeta) (*MFAEnrollment, error) {
	secret, err := totp.GenerateSecret(rand.Reader)
	if err != nil {
		return nil, err
	}
	enc, err := s.cipher.Encrypt(secret, pendingAAD(user.ID))
	if err != nil {
		return nil, err
	}
	if err := s.mfa.SetPendingMFASecret(ctx, user.ID, enc); err != nil {
		return nil, err
	}
	s.record(ctx, user.ID, user, "auth.mfa_enroll_started", meta, nil)
	return &MFAEnrollment{Secret: secret, OTPAuthURL: totp.URL(s.opts.Issuer, user.Email, secret)}, nil
}

func (s *Service) confirmEnrollment(ctx context.Context, user *identity.User, st *SecurityState, code string, meta requestMeta) ([]string, error) {
	if st.MFAPendingEnc == "" {
		return nil, shared.NewInvalidState("mfa enrollment not started")
	}
	secret, err := s.cipher.Decrypt(st.MFAPendingEnc, pendingAAD(user.ID))
	if err != nil {
		return nil, err
	}
	step, err := totp.Verify(secret, code, s.now(), 0)
	if err != nil {
		s.record(ctx, user.ID, user, "auth.mfa_confirm_failed", meta, nil)
		return nil, errBadSecondFactor
	}
	enc, err := s.cipher.Encrypt(secret, secretAAD(user.ID))
	if err != nil {
		return nil, err
	}
	codes, hashes, err := authsec.NewRecoveryCodes(rand.Reader)
	if err != nil {
		return nil, err
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.mfa.ActivateMFA(ctx, user.ID, enc, step, s.now()); err != nil {
			return err
		}
		return s.mfa.ReplaceRecoveryCodes(ctx, user.ID, hashes)
	})
	if err != nil {
		return nil, err
	}
	s.record(ctx, user.ID, user, "auth.mfa_enabled", meta, nil)
	return codes, nil
}

// checkSecondFactor accepts a TOTP code (replay-protected per step) or, when
// allowRecovery, a one-time recovery code which is consumed.
func (s *Service) checkSecondFactor(ctx context.Context, userID uuid.UUID, st *SecurityState, code string, allowRecovery bool) (string, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", errBadSecondFactor
	}
	if totp.LooksLikeCode(code) {
		secret, err := s.cipher.Decrypt(st.MFASecretEnc, secretAAD(userID))
		if err != nil {
			return "", err
		}
		step, err := totp.Verify(secret, code, s.now(), st.MFALastStep)
		if err != nil {
			return "", errBadSecondFactor
		}
		ok, err := s.mfa.AdvanceMFAStep(ctx, userID, step)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errBadSecondFactor
		}
		return methodTOTP, nil
	}
	if !allowRecovery {
		return "", errBadSecondFactor
	}
	ok, err := s.mfa.ConsumeRecoveryCode(ctx, userID, authsec.HashRecoveryCode(code), s.now())
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errBadSecondFactor
	}
	return methodRecovery, nil
}
