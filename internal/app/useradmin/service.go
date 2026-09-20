package useradmin

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Service struct {
	users identity.Repository
	audit audit.Recorder
	tx    *tx.Manager
}

func NewService(users identity.Repository, auditRec audit.Recorder, txm *tx.Manager) *Service {
	return &Service{users: users, audit: auditRec, tx: txm}
}

type CreateInput struct {
	Email     string
	Password  string
	FullName  string
	Role      platformauth.Role
	BranchID  uuid.UUID
	TeamID    *uuid.UUID
	ActorID   uuid.UUID
	IP        string
	UserAgent string
}

type UpdateInput struct {
	UserID     uuid.UUID
	FullName   *string
	Role       *platformauth.Role
	BranchID   *uuid.UUID
	SetTeam    bool
	TeamID     *uuid.UUID // used when SetTeam=true; nil clears
	IsActive   *bool
	Password   *string
	MFAEnabled *bool
	ActorID    uuid.UUID
	IP         string
	UserAgent  string
}

func (s *Service) List(ctx context.Context, f identity.UserFilter) ([]identity.User, int, error) {
	return s.users.ListUsers(ctx, f)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*identity.User, error) {
	u, err := s.users.FindUserByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("user")
	}
	return u, nil
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*identity.User, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if email == "" || in.FullName == "" {
		return nil, shared.NewValidation("email and full_name are required")
	}
	if !platformauth.ValidRole(in.Role) {
		return nil, shared.NewValidation("invalid role")
	}
	if err := platformauth.ValidatePassword(in.Password); err != nil {
		return nil, shared.NewValidation(err.Error())
	}
	if in.BranchID == uuid.Nil {
		return nil, shared.NewValidation("branch_id is required")
	}
	if in.TeamID != nil {
		if _, err := s.users.FindTeam(ctx, *in.TeamID); err != nil {
			return nil, shared.NewNotFound("team")
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	u := &identity.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: string(hash),
		FullName:     strings.TrimSpace(in.FullName),
		Role:         in.Role,
		BranchID:     in.BranchID,
		TeamID:       in.TeamID,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.users.CreateUser(ctx, u); err != nil {
			return shared.NewConflict("email already exists")
		}
		id := u.ID
		branch := u.BranchID
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID:    in.ActorID,
			Action:     "user.created",
			EntityType: "user",
			EntityID:   &id,
			BranchID:   &branch,
			After: map[string]any{
				"email": u.Email, "role": u.Role, "branch_id": u.BranchID, "team_id": u.TeamID,
			},
			IP: in.IP, UserAgent: in.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*identity.User, error) {
	u, err := s.users.FindUserByID(ctx, in.UserID)
	if err != nil {
		return nil, shared.NewNotFound("user")
	}
	before := map[string]any{
		"email": u.Email, "role": u.Role, "branch_id": u.BranchID, "team_id": u.TeamID,
		"is_active": u.IsActive, "mfa_enabled": u.MFAEnabled,
	}
	if in.FullName != nil {
		u.FullName = strings.TrimSpace(*in.FullName)
	}
	if in.Role != nil {
		if !platformauth.ValidRole(*in.Role) {
			return nil, shared.NewValidation("invalid role")
		}
		u.Role = *in.Role
	}
	if in.BranchID != nil {
		u.BranchID = *in.BranchID
	}
	if in.SetTeam {
		u.TeamID = in.TeamID
		if u.TeamID != nil {
			if _, err := s.users.FindTeam(ctx, *u.TeamID); err != nil {
				return nil, shared.NewNotFound("team")
			}
		}
	}
	if in.IsActive != nil {
		u.IsActive = *in.IsActive
	}
	if in.MFAEnabled != nil {
		u.MFAEnabled = *in.MFAEnabled
	}
	if in.Password != nil {
		if err := platformauth.ValidatePassword(*in.Password); err != nil {
			return nil, shared.NewValidation(err.Error())
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*in.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = string(hash)
	}
	u.UpdatedAt = time.Now().UTC()
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.users.UpdateUser(ctx, u); err != nil {
			return err
		}
		id := u.ID
		branch := u.BranchID
		return s.audit.Record(ctx, audit.RecordInput{
			ActorID: in.ActorID, Action: "user.updated", EntityType: "user", EntityID: &id, BranchID: &branch,
			Before: before,
			After: map[string]any{
				"email": u.Email, "role": u.Role, "branch_id": u.BranchID, "team_id": u.TeamID,
				"is_active": u.IsActive, "mfa_enabled": u.MFAEnabled,
			},
			IP: in.IP, UserAgent: in.UserAgent,
		})
	})
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Service) ListBranches(ctx context.Context) ([]identity.Branch, error) {
	return s.users.ListBranches(ctx)
}

func (s *Service) ListTeams(ctx context.Context, branchID *uuid.UUID) ([]identity.Team, error) {
	return s.users.ListTeams(ctx, branchID)
}
