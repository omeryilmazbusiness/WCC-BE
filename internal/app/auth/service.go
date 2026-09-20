package auth

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	appaudit "github.com/wodi-crm/wodi-crm-be/internal/app/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type LoginInput struct {
	Email     string
	Password  string
	IP        string
	UserAgent string
}

type LoginResult struct {
	Tokens       *platformauth.TokenPair `json:"tokens,omitempty"`
	User         *identity.User          `json:"user,omitempty"`
	MFARequired  bool                    `json:"mfa_required"`
	MFAChallenge string                  `json:"mfa_challenge,omitempty"`
}

type MFAVerifyInput struct {
	Challenge string
	Code      string
	IP        string
	UserAgent string
}

type LogoutInput struct {
	UserID    uuid.UUID
	BranchID  uuid.UUID
	IP        string
	UserAgent string
}

type Service struct {
	users  identity.Repository
	audit  *appaudit.Service
	tokens *platformauth.TokenService
	tx     *tx.Manager
	mfa    platformauth.MFAProvider
}

func NewService(
	users identity.Repository,
	auditSvc *appaudit.Service,
	tokens *platformauth.TokenService,
	txm *tx.Manager,
	mfa platformauth.MFAProvider,
) *Service {
	if mfa == nil {
		mfa = platformauth.NewPolicyMFA() // no forced roles by default
	}
	return &Service{users: users, audit: auditSvc, tokens: tokens, tx: txm, mfa: mfa}
}

func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if email == "" || in.Password == "" {
		return nil, shared.NewValidation("email and password are required")
	}

	user, err := s.users.FindUserByEmail(ctx, email)
	if err != nil {
		return nil, shared.NewUnauthorized("invalid credentials")
	}
	if !user.IsActive {
		return nil, shared.NewForbidden("user is inactive")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)); err != nil {
		return nil, shared.NewUnauthorized("invalid credentials")
	}

	if s.mfa.IsEnabled(user.ID, user.Role, user.MFAEnabled) {
		chal, err := s.mfa.StartChallenge(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		return &LoginResult{MFARequired: true, MFAChallenge: chal, User: publicUser(user)}, nil
	}

	pair, err := s.issueFor(user)
	if err != nil {
		return nil, err
	}
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: user.ID, Action: "auth.login", EntityType: "user", EntityID: &user.ID, BranchID: &user.BranchID,
		IP: in.IP, UserAgent: in.UserAgent,
	})
	return &LoginResult{Tokens: &pair, User: publicUser(user)}, nil
}

func (s *Service) VerifyMFA(ctx context.Context, in MFAVerifyInput) (*LoginResult, error) {
	uid, err := s.mfa.Verify(ctx, in.Challenge, in.Code)
	if err != nil {
		return nil, shared.NewUnauthorized("invalid mfa code")
	}
	user, err := s.users.FindUserByID(ctx, uid)
	if err != nil || !user.IsActive {
		return nil, shared.NewUnauthorized("invalid mfa code")
	}
	pair, err := s.issueFor(user)
	if err != nil {
		return nil, err
	}
	_ = s.audit.Record(ctx, audit.RecordInput{
		ActorID: user.ID, Action: "auth.mfa_verified", EntityType: "user", EntityID: &user.ID, BranchID: &user.BranchID,
		IP: in.IP, UserAgent: in.UserAgent,
	})
	return &LoginResult{Tokens: &pair, User: publicUser(user)}, nil
}

func (s *Service) Logout(ctx context.Context, in LogoutInput) error {
	return s.audit.Record(ctx, audit.RecordInput{
		ActorID: in.UserID, Action: "auth.logout", EntityType: "user", EntityID: &in.UserID, BranchID: &in.BranchID,
		IP: in.IP, UserAgent: in.UserAgent,
	})
}

func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*identity.User, error) {
	user, err := s.users.FindUserByID(ctx, userID)
	if err != nil {
		return nil, shared.NewNotFound("user")
	}
	return publicUser(user), nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*platformauth.TokenPair, error) {
	claims, err := s.tokens.ParseRefresh(refreshToken)
	if err != nil {
		return nil, shared.NewUnauthorized("invalid refresh token")
	}
	uid, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, shared.NewUnauthorized("invalid refresh token")
	}
	user, err := s.users.FindUserByID(ctx, uid)
	if err != nil || !user.IsActive {
		return nil, shared.NewUnauthorized("invalid refresh token")
	}
	pair, err := s.issueFor(user)
	if err != nil {
		return nil, err
	}
	return &pair, nil
}

func (s *Service) issueFor(user *identity.User) (platformauth.TokenPair, error) {
	teamID := uuid.Nil
	if user.TeamID != nil {
		teamID = *user.TeamID
	}
	return s.tokens.Issue(user.ID, user.Email, user.Role, user.BranchID, teamID)
}

func publicUser(u *identity.User) *identity.User {
	cp := *u
	cp.PasswordHash = ""
	return &cp
}
