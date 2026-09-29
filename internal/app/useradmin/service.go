package useradmin

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// SessionInvalidator drops cached access-token checks of a user whose
// credentials or claims changed; the database bumps token_version and, for
// password changes and deactivation, revokes the sessions.
type SessionInvalidator interface {
	InvalidateUser(userID uuid.UUID)
}

type Service struct {
	users    identity.Repository
	audit    audit.Recorder
	tx       *tx.Manager
	sessions SessionInvalidator
}

func NewService(users identity.Repository, auditRec audit.Recorder, txm *tx.Manager, sessions SessionInvalidator) *Service {
	return &Service{users: users, audit: auditRec, tx: txm, sessions: sessions}
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
	return s.visibleUser(ctx, id)
}

// visibleUser loads a user through the unscoped identity lookup and hides it
// unless the caller's scope covers the user's branch.
func (s *Service) visibleUser(ctx context.Context, id uuid.UUID) (*identity.User, error) {
	u, err := s.users.FindUserByID(ctx, id)
	if err != nil || !access.From(ctx).CanAccessBranch(u.BranchID) {
		return nil, shared.NewNotFound("user")
	}
	return u, nil
}

func (s *Service) checkTeam(ctx context.Context, teamID, branchID uuid.UUID) error {
	t, err := s.users.FindTeam(ctx, teamID)
	if err != nil {
		return shared.NewNotFound("team")
	}
	if t.BranchID != branchID {
		return shared.NewValidation("team belongs to another branch")
	}
	return nil
}

// checkPlacement enforces the tenancy rules of a user assignment: the branch
// must be inside the caller's reach, a GM can only be placed by a
// company-wide caller and only platform scope may grant the admin role.
func checkPlacement(ctx context.Context, role platformauth.Role, branchID uuid.UUID) error {
	sc := access.From(ctx)
	if !sc.CanAccessBranch(branchID) {
		err := shared.NewValidation("branch outside your company")
		err.Details = map[string]any{"branch_id": "not in your company"}
		return err
	}
	switch role {
	case platformauth.RoleAdmin:
		if sc.Level != access.LevelGlobal {
			return shared.NewForbidden("only platform operators can grant the admin role")
		}
	case platformauth.RoleGM:
		if sc.Level < access.LevelCompany {
			return shared.NewForbidden("only a company-wide account can grant the gm role")
		}
	}
	return nil
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
	if err := checkPlacement(ctx, in.Role, in.BranchID); err != nil {
		return nil, err
	}
	if in.TeamID != nil {
		if err := s.checkTeam(ctx, *in.TeamID, in.BranchID); err != nil {
			return nil, err
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
	u, err := s.visibleUser(ctx, in.UserID)
	if err != nil {
		return nil, err
	}
	before := map[string]any{
		"email": u.Email, "role": u.Role, "branch_id": u.BranchID, "team_id": u.TeamID,
		"is_active": u.IsActive, "mfa_enabled": u.MFAEnabled,
	}
	if u.Role == platformauth.RoleAdmin && access.From(ctx).Level != access.LevelGlobal {
		return nil, shared.NewForbidden("platform accounts are managed by platform operators")
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
	if in.Role != nil || in.BranchID != nil {
		if err := checkPlacement(ctx, u.Role, u.BranchID); err != nil {
			return nil, err
		}
	}
	if in.SetTeam {
		u.TeamID = in.TeamID
	}
	if u.TeamID != nil && (in.SetTeam || in.BranchID != nil) {
		if err := s.checkTeam(ctx, *u.TeamID, u.BranchID); err != nil {
			return nil, err
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
	s.sessions.InvalidateUser(u.ID)
	return u, nil
}

func (s *Service) ListTeams(ctx context.Context, branchID *uuid.UUID) ([]identity.Team, error) {
	return s.users.ListTeams(ctx, branchID)
}
