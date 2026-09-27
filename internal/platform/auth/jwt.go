package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/config"
)

type Role string

// Claims carried in access tokens for RBAC + scope. sid binds the token to a
// server-side session and ver to the user's token version, so revocation and
// role/branch changes take effect before exp.
type Claims struct {
	UserID       uuid.UUID `json:"uid"`
	Email        string    `json:"email"`
	Role         Role      `json:"role"`
	BranchID     uuid.UUID `json:"branch_id"`
	TeamID       uuid.UUID `json:"team_id,omitempty"`
	SessionID    uuid.UUID `json:"sid"`
	TokenVersion int       `json:"ver"`
	jwt.RegisteredClaims
}

// TokenPair is what a successful login or refresh hands to the client.
type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        int64
	RefreshExpiresAt time.Time
}

// AccessSubject is the identity an access token is issued for.
type AccessSubject struct {
	UserID       uuid.UUID
	Email        string
	Role         Role
	BranchID     uuid.UUID
	TeamID       uuid.UUID
	SessionID    uuid.UUID
	TokenVersion int
}

type AccessToken struct {
	Token     string
	ExpiresIn int64
	ExpiresAt time.Time
}

// TokenService issues and parses short-lived access tokens.
type TokenService struct {
	signer   TokenSigner
	ttl      time.Duration
	issuer   string
	audience string
	now      func() time.Time
}

func NewTokenService(signer TokenSigner, ttl time.Duration, issuer, audience string) *TokenService {
	return &TokenService{
		signer: signer, ttl: ttl, issuer: issuer, audience: audience,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// NewTokenServiceFromConfig builds the HS256 keyring: JWT_ACCESS_SECRET under
// JWT_ACCESS_KEY_ID signs; JWT_PREVIOUS_ACCESS_SECRETS only verify.
func NewTokenServiceFromConfig(cfg config.AuthConfig) (*TokenService, error) {
	active, err := NewHS256Key(cfg.JWTAccessKeyID, cfg.JWTAccessSecret)
	if err != nil {
		return nil, err
	}
	previous, err := ParseHS256Keys(cfg.JWTPreviousAccessSecrets)
	if err != nil {
		return nil, err
	}
	signer, err := NewJWTSigner(active, previous, Validation{
		Issuer: cfg.Issuer, Audience: cfg.Audience, Leeway: DefaultLeeway,
	})
	if err != nil {
		return nil, err
	}
	return NewTokenService(signer, cfg.AccessTTL, cfg.Issuer, cfg.Audience), nil
}

// ParseHS256Keys parses "kid:secret" entries.
func ParseHS256Keys(entries []string) ([]SigningKey, error) {
	out := make([]SigningKey, 0, len(entries))
	for _, e := range entries {
		kid, secret, ok := strings.Cut(strings.TrimSpace(e), ":")
		if !ok {
			return nil, fmt.Errorf("previous access secret must be kid:secret")
		}
		k, err := NewHS256Key(strings.TrimSpace(kid), secret)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func (s *TokenService) IssueAccess(sub AccessSubject) (AccessToken, error) {
	now := s.now()
	exp := now.Add(s.ttl)
	token, err := s.signer.Sign(&Claims{
		UserID:       sub.UserID,
		Email:        sub.Email,
		Role:         sub.Role,
		BranchID:     sub.BranchID,
		TeamID:       sub.TeamID,
		SessionID:    sub.SessionID,
		TokenVersion: sub.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   sub.UserID.String(),
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        uuid.NewString(),
		},
	})
	if err != nil {
		return AccessToken{}, fmt.Errorf("sign access: %w", err)
	}
	return AccessToken{Token: token, ExpiresIn: int64(s.ttl.Seconds()), ExpiresAt: exp}, nil
}

func (s *TokenService) ParseAccess(token string) (*Claims, error) {
	c, err := s.signer.Verify(token)
	if err != nil {
		return nil, err
	}
	if c.UserID == uuid.Nil || c.SessionID == uuid.Nil || c.Subject != c.UserID.String() {
		return nil, errors.New("access token lacks subject or session")
	}
	return c, nil
}
