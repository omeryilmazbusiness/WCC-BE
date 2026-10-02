package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/authsec"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/company"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/crypto"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/ratelimit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

const (
	LoginChallengeTTL    = 5 * time.Minute
	EnrollmentTokenTTL   = 15 * time.Minute
	maxChallengeAttempts = 5
	maxUserAgentLen      = 512
)

type LoginInput struct {
	Email    string
	Password string
	// Company is the slug of the company sign-in page used; empty for the
	// generic page. A user of another company gets invalid credentials.
	Company   string
	IP        string
	UserAgent string
}

// LoginResult is exactly one of: tokens issued, MFA challenge pending, or
// MFA enrollment required (forced role without a second factor yet).
type LoginResult struct {
	Tokens                *platformauth.TokenPair `json:"tokens,omitempty"`
	User                  *identity.User          `json:"user,omitempty"`
	MFARequired           bool                    `json:"mfa_required"`
	MFAChallenge          string                  `json:"mfa_challenge,omitempty"`
	MFAEnrollmentRequired bool                    `json:"mfa_enrollment_required"`
	EnrollmentToken       string                  `json:"enrollment_token,omitempty"`
	ExpiresIn             int                     `json:"expires_in,omitempty"`
	RecoveryCodes         []string                `json:"recovery_codes,omitempty"`
}

type MFAVerifyInput struct {
	Challenge string
	Code      string
	IP        string
	UserAgent string
}

type RefreshInput struct {
	RefreshToken string
	IP           string
	UserAgent    string
}

type LogoutInput struct {
	UserID       uuid.UUID
	BranchID     uuid.UUID
	SessionID    uuid.UUID
	RefreshToken string
	IP           string
	UserAgent    string
}

type UnlockInput struct {
	ActorID   uuid.UUID
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

// Options carries security policy; zero values disable the related limit.
type Options struct {
	Lockout       authsec.LockoutPolicy
	IPMaxAttempts int
	MFA           platformauth.MFAPolicy
	Issuer        string
	// Session bounds refresh (idle) and session (absolute) lifetimes.
	Session authsec.SessionPolicy
	// ReuseGrace tolerates a just-rotated refresh token; 0 disables.
	ReuseGrace time.Duration
}

// Deps are the collaborators of Service.
type Deps struct {
	Users      identity.Repository
	Audit      audit.Recorder
	Tokens     AccessTokenIssuer
	Tx         tx.Runner
	Lockouts   LockoutStore
	MFA        MFAStore
	Challenges ChallengeStore
	Refresh    RefreshStore
	Sessions   SessionStore
	// SessionCache is told about every revocation; nil when nothing caches.
	SessionCache SessionInvalidator
	Cipher       crypto.Cipher
	Limiter      ratelimit.Window
	// Companies resolves a user's company for company sign-in pages; nil
	// disables the check.
	Companies company.Directory
	Options   Options
}

type Service struct {
	users      identity.Repository
	audit      audit.Recorder
	tokens     AccessTokenIssuer
	tx         tx.Runner
	lockouts   LockoutStore
	mfa        MFAStore
	challenges ChallengeStore
	refresh    RefreshStore
	sessions   SessionStore
	cache      SessionInvalidator
	cipher     crypto.Cipher
	limiter    ratelimit.Window
	opts       Options
	companies  company.Directory
	now        func() time.Time
}

const (
	defaultIdleTTL     = 7 * 24 * time.Hour
	defaultAbsoluteTTL = 30 * 24 * time.Hour
)

func NewService(d Deps) *Service {
	if d.Limiter == nil {
		d.Limiter = ratelimit.NewMemory()
	}
	if d.Tx == nil {
		d.Tx = tx.Nop{}
	}
	if d.SessionCache == nil {
		d.SessionCache = noopInvalidator{}
	}
	if d.Options.Issuer == "" {
		d.Options.Issuer = "Wodi CRM"
	}
	if d.Options.Session.Idle <= 0 {
		d.Options.Session.Idle = defaultIdleTTL
	}
	if d.Options.Session.Absolute <= 0 {
		d.Options.Session.Absolute = defaultAbsoluteTTL
	}
	return &Service{
		users: d.Users, audit: d.Audit, tokens: d.Tokens, tx: d.Tx,
		lockouts: d.Lockouts, mfa: d.MFA, challenges: d.Challenges, refresh: d.Refresh,
		sessions: d.Sessions, cache: d.SessionCache,
		cipher: d.Cipher, limiter: d.Limiter, opts: d.Options, companies: d.Companies,
		now: func() time.Time { return time.Now().UTC() },
	}
}

var (
	errInvalidCredentials = shared.NewUnauthorized("invalid credentials")
	errInvalidRefresh     = shared.NewUnauthorized("invalid refresh token")
	errInvalidChallenge   = shared.NewUnauthorized("invalid or expired mfa challenge")
	errInvalidMFACode     = shared.NewUnauthorized("invalid mfa code")
	errRefreshRaced       = errors.New("refresh token already rotated")
)

type requestMeta struct {
	IP        string
	UserAgent string
}

func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	email := normalizeEmail(in.Email)
	if email == "" || in.Password == "" {
		return nil, shared.NewValidation("email and password are required")
	}
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	if err := s.checkIPThrottle(ctx, meta.IP); err != nil {
		return nil, err
	}

	user, err := s.users.FindUserByEmail(ctx, email)
	if err != nil || user == nil {
		return nil, s.unknownUserFailure(ctx, email, in.Password, meta)
	}
	st, err := s.lockouts.SecurityState(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, errInvalidCredentials
	}
	if wait := authsec.RetryAfter(st.LockedUntil, s.now()); wait > 0 {
		return nil, lockedError(wait)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)) != nil {
		return nil, s.registerFailure(ctx, user, email, meta, "password", errInvalidCredentials)
	}
	if !user.IsActive {
		return nil, shared.NewForbidden("user is inactive")
	}
	if !s.belongsTo(ctx, user, in.Company) {
		// The password was right, so this is not a guessing failure and does not
		// count towards lockout; the answer still reveals nothing.
		s.record(ctx, user.ID, user, "auth.login_wrong_company", meta, map[string]any{"company": in.Company})
		return nil, errInvalidCredentials
	}

	switch {
	case st.MFAEnabled && st.MFASecretEnc != "":
		token, err := s.newChallenge(ctx, user.ID, authsec.ChallengeLogin, LoginChallengeTTL, meta.IP)
		if err != nil {
			return nil, err
		}
		s.record(ctx, user.ID, user, "auth.mfa_challenge_issued", meta, nil)
		return &LoginResult{
			MFARequired: true, MFAChallenge: token, ExpiresIn: int(LoginChallengeTTL.Seconds()),
			User: publicUser(user, st),
		}, nil
	case st.MFAEnabled || s.opts.MFA.Required(user.Role):
		token, err := s.newChallenge(ctx, user.ID, authsec.ChallengeEnroll, EnrollmentTokenTTL, meta.IP)
		if err != nil {
			return nil, err
		}
		s.record(ctx, user.ID, user, "auth.mfa_enrollment_required", meta, nil)
		return &LoginResult{
			MFAEnrollmentRequired: true, EnrollmentToken: token, ExpiresIn: int(EnrollmentTokenTTL.Seconds()),
			User: publicUser(user, st),
		}, nil
	}

	pair, err := s.completeLogin(ctx, user, email, meta, "password")
	if err != nil {
		return nil, err
	}
	return &LoginResult{Tokens: pair, User: publicUser(user, st)}, nil
}

// belongsTo reports whether user may sign in on the given company page; the
// generic page (empty slug) admits everyone.
func (s *Service) belongsTo(ctx context.Context, user *identity.User, companySlug string) bool {
	slug := strings.ToLower(strings.TrimSpace(companySlug))
	if slug == "" || s.companies == nil {
		return true
	}
	if user.BranchID == uuid.Nil {
		return false
	}
	got, err := s.companies.CompanySlugOfBranch(ctx, user.BranchID)
	return err == nil && got == slug
}

func (s *Service) VerifyMFA(ctx context.Context, in MFAVerifyInput) (*LoginResult, error) {
	if strings.TrimSpace(in.Challenge) == "" || strings.TrimSpace(in.Code) == "" {
		return nil, shared.NewValidation("challenge and code are required")
	}
	meta := requestMeta{IP: in.IP, UserAgent: in.UserAgent}
	if err := s.checkIPThrottle(ctx, meta.IP); err != nil {
		return nil, err
	}
	now := s.now()
	hash := authsec.HashToken(in.Challenge)
	ch, err := s.challenges.AttemptChallenge(ctx, hash, authsec.ChallengeLogin, maxChallengeAttempts, now)
	if err != nil {
		return nil, err
	}
	if ch == nil {
		return nil, errInvalidChallenge
	}
	user, st, err := s.loadUser(ctx, ch.UserID)
	if err != nil || !user.IsActive || !st.MFAEnabled || st.MFASecretEnc == "" {
		_ = s.challenges.DeleteChallenge(ctx, hash)
		return nil, errInvalidChallenge
	}
	if wait := authsec.RetryAfter(st.LockedUntil, now); wait > 0 {
		_ = s.challenges.DeleteChallenge(ctx, hash)
		return nil, lockedError(wait)
	}

	method, err := s.checkSecondFactor(ctx, user.ID, st, in.Code, true)
	if err != nil {
		if !errors.Is(err, errBadSecondFactor) {
			return nil, err
		}
		s.record(ctx, user.ID, user, "auth.mfa_failed", meta, map[string]any{"attempt": ch.Attempts})
		if ch.Attempts >= maxChallengeAttempts {
			_ = s.challenges.DeleteChallenge(ctx, hash)
		}
		return nil, s.registerFailure(ctx, user, user.Email, meta, "mfa", errInvalidMFACode)
	}
	_ = s.challenges.DeleteChallenge(ctx, hash)
	s.recordFactorUse(ctx, user, method, meta)

	pair, err := s.completeLogin(ctx, user, user.Email, meta, method)
	if err != nil {
		return nil, err
	}
	return &LoginResult{Tokens: pair, User: publicUser(user, st)}, nil
}

// Unlock clears a login lockout (admin action, PermUsersUnlock).
func (s *Service) Unlock(ctx context.Context, in UnlockInput) error {
	user, err := s.users.FindUserByID(ctx, in.UserID)
	if err != nil || user == nil {
		return shared.NewNotFound("user")
	}
	if err := s.lockouts.UnlockUser(ctx, user.ID); err != nil {
		return err
	}
	_ = s.limiter.Reset(ctx, emailKey(normalizeEmail(user.Email)))
	s.record(ctx, in.ActorID, user, "auth.account_unlocked", requestMeta{IP: in.IP, UserAgent: in.UserAgent}, nil)
	return nil
}

func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*identity.User, error) {
	user, st, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, shared.NewNotFound("user")
	}
	return publicUser(user, st), nil
}

func (s *Service) newChallenge(ctx context.Context, userID uuid.UUID, purpose string, ttl time.Duration, ip string) (string, error) {
	raw, err := authsec.NewOpaqueToken(rand.Reader, 32)
	if err != nil {
		return "", err
	}
	err = s.challenges.CreateChallenge(ctx, authsec.Challenge{
		TokenHash: authsec.HashToken(raw), UserID: userID, Purpose: purpose,
		ExpiresAt: s.now().Add(ttl), CreatedIP: ip,
	})
	if err != nil {
		return "", err
	}
	return raw, nil
}

func (s *Service) loadUser(ctx context.Context, id uuid.UUID) (*identity.User, *SecurityState, error) {
	user, err := s.users.FindUserByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if user == nil {
		return nil, nil, shared.NewNotFound("user")
	}
	st, err := s.lockouts.SecurityState(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, shared.NewNotFound("user")
	}
	return user, st, nil
}

func (s *Service) record(ctx context.Context, actor uuid.UUID, user *identity.User, action string, meta requestMeta, extra map[string]any) {
	id, branch := user.ID, user.BranchID
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: actor, Action: action, EntityType: "user", EntityID: &id, BranchID: &branch,
		IP: meta.IP, UserAgent: meta.UserAgent, Extra: extra,
	})
}

func normalizeEmail(e string) string {
	return strings.TrimSpace(strings.ToLower(e))
}

func publicUser(u *identity.User, st *SecurityState) *identity.User {
	cp := *u
	cp.PasswordHash = ""
	if st != nil {
		cp.MFAEnabled = st.MFAEnabled && st.MFASecretEnc != ""
	}
	return &cp
}
