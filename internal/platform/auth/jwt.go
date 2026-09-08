package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

type Role string

const (
	RoleGM       Role = "gm"
	RoleManager  Role = "manager"
	RoleEmployee Role = "employee"
)

// Claims carried in access tokens for RBAC + scope.
type Claims struct {
	UserID   uuid.UUID `json:"uid"`
	Email    string    `json:"email"`
	Role     Role      `json:"role"`
	BranchID uuid.UUID `json:"branch_id"`
	TeamID   uuid.UUID `json:"team_id,omitempty"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

type TokenService struct {
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	issuer        string
}

func NewTokenService(cfg config.AuthConfig) *TokenService {
	return &TokenService{
		accessSecret:  []byte(cfg.JWTAccessSecret),
		refreshSecret: []byte(cfg.JWTRefreshSecret),
		accessTTL:     cfg.AccessTTL,
		refreshTTL:    cfg.RefreshTTL,
		issuer:        cfg.Issuer,
	}
}

func (s *TokenService) Issue(userID uuid.UUID, email string, role Role, branchID, teamID uuid.UUID) (TokenPair, error) {
	now := time.Now().UTC()
	accessClaims := Claims{
		UserID:   userID,
		Email:    email,
		Role:     role,
		BranchID: branchID,
		TeamID:   teamID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
			ID:        uuid.NewString(),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.accessSecret)
	if err != nil {
		return TokenPair{}, fmt.Errorf("sign access: %w", err)
	}

	refreshClaims := jwt.RegisteredClaims{
		Issuer:    s.issuer,
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
		ID:        uuid.NewString(),
	}
	refresh, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.refreshSecret)
	if err != nil {
		return TokenPair{}, fmt.Errorf("sign refresh: %w", err)
	}

	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
	}, nil
}

func (s *TokenService) ParseAccess(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.accessSecret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	return claims, nil
}

func (s *TokenService) ParseRefresh(token string) (*jwt.RegisteredClaims, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.refreshSecret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid {
		return nil, fmt.Errorf("invalid refresh token")
	}
	return claims, nil
}
