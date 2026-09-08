package auth

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

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
	Tokens platformauth.TokenPair
	User   *identity.User
}

type Service struct {
	users  identity.Repository
	audit  audit.Repository
	tokens *platformauth.TokenService
	tx     *tx.Manager
}

func NewService(
	users identity.Repository,
	auditRepo audit.Repository,
	tokens *platformauth.TokenService,
	txm *tx.Manager,
) *Service {
	return &Service{users: users, audit: auditRepo, tokens: tokens, tx: txm}
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

	teamID := uuid.Nil
	if user.TeamID != nil {
		teamID = *user.TeamID
	}
	pair, err := s.tokens.Issue(user.ID, user.Email, user.Role, user.BranchID, teamID)
	if err != nil {
		return nil, err
	}

	_ = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		actor := user.ID
		return s.audit.Insert(ctx, &audit.Event{
			ID:         uuid.New(),
			ActorID:    &actor,
			Action:     "auth.login",
			EntityType: "user",
			EntityID:   &actor,
			BranchID:   &user.BranchID,
			IP:         in.IP,
			UserAgent:  in.UserAgent,
		})
	})

	return &LoginResult{Tokens: pair, User: user}, nil
}

func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*identity.User, error) {
	user, err := s.users.FindUserByID(ctx, userID)
	if err != nil {
		return nil, shared.NewNotFound("user")
	}
	return user, nil
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
	teamID := uuid.Nil
	if user.TeamID != nil {
		teamID = *user.TeamID
	}
	pair, err := s.tokens.Issue(user.ID, user.Email, user.Role, user.BranchID, teamID)
	if err != nil {
		return nil, err
	}
	return &pair, nil
}

// HashPassword is used by migrate/seed tooling.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}
