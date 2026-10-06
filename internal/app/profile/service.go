// Package profile lets a signed-in user maintain their own details: name,
// job title, phone, sign-in email and photo. Role, branch and team stay with
// user administration.
package profile

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// Users is the part of the identity store the profile needs.
type Users interface {
	FindUserByID(ctx context.Context, id uuid.UUID) (*identity.User, error)
	UpdateUser(ctx context.Context, user *identity.User) error
	FindTeam(ctx context.Context, id uuid.UUID) (*identity.Team, error)
}

// SessionInvalidator drops cached access-token checks after an email change,
// since access tokens carry the email.
type SessionInvalidator interface {
	InvalidateUser(userID uuid.UUID)
}

// Actor is the signed-in user editing their own profile.
type Actor struct {
	UserID    uuid.UUID
	IP        string
	UserAgent string
}

// Profile is the caller's account with the name of their team, if any.
type Profile struct {
	User identity.User
	Team *identity.Team
}

// UpdateInput changes only the fields that are set; an empty string clears
// phone and job title.
type UpdateInput struct {
	FullName *string
	JobTitle *string
	Phone    *string
}

type ChangeEmailInput struct {
	Email    string
	Password string
}

type Service struct {
	users    Users
	avatars  identity.AvatarStore
	audit    audit.Recorder
	tx       tx.Runner
	sessions SessionInvalidator
	now      func() time.Time
}

func NewService(users Users, avatars identity.AvatarStore, rec audit.Recorder, txr tx.Runner, sessions SessionInvalidator) *Service {
	if txr == nil {
		txr = tx.Nop{}
	}
	return &Service{
		users: users, avatars: avatars, audit: rec, tx: txr, sessions: sessions,
		now: func() time.Time { return time.Now().UTC() },
	}
}

func errWrongPassword() error {
	err := shared.NewValidation("current password is incorrect")
	err.Details = map[string]any{"password": "incorrect"}
	return err
}

func (s *Service) Get(ctx context.Context, userID uuid.UUID) (*Profile, error) {
	u, err := s.load(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.profileOf(ctx, u), nil
}

func (s *Service) Update(ctx context.Context, a Actor, in UpdateInput) (*Profile, error) {
	u, err := s.load(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	before := map[string]any{"full_name": u.FullName, "job_title": u.JobTitle, "phone": u.Phone}
	changed := []string{}
	if in.FullName != nil {
		name, err := identity.NormalizeFullName(*in.FullName)
		if err != nil {
			return nil, err
		}
		if name != u.FullName {
			u.FullName, changed = name, append(changed, "full_name")
		}
	}
	if in.JobTitle != nil {
		title, err := identity.NormalizeJobTitle(*in.JobTitle)
		if err != nil {
			return nil, err
		}
		if title != u.JobTitle {
			u.JobTitle, changed = title, append(changed, "job_title")
		}
	}
	if in.Phone != nil {
		phone, err := identity.NormalizePhone(*in.Phone)
		if err != nil {
			return nil, err
		}
		if phone != u.Phone {
			u.Phone, changed = phone, append(changed, "phone")
		}
	}
	if len(changed) == 0 {
		return s.profileOf(ctx, u), nil
	}
	u.UpdatedAt = s.now()
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.users.UpdateUser(ctx, u); err != nil {
			return err
		}
		after := map[string]any{"full_name": u.FullName, "job_title": u.JobTitle, "phone": u.Phone}
		return s.record(ctx, a, u, "user.profile_updated", only(before, changed), only(after, changed), map[string]any{"fields": changed})
	})
	if err != nil {
		return nil, err
	}
	return s.profileOf(ctx, u), nil
}

// ChangeEmail moves the sign-in address after re-checking the password, so a
// left-open session cannot take the account over.
func (s *Service) ChangeEmail(ctx context.Context, a Actor, in ChangeEmailInput) (*Profile, error) {
	email, err := identity.NormalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	u, err := s.load(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if in.Password == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Password)) != nil {
		return nil, errWrongPassword()
	}
	if email == u.Email {
		err := shared.NewValidation("this is already your email")
		err.Details = map[string]any{"email": "unchanged"}
		return nil, err
	}
	previous := u.Email
	u.Email, u.UpdatedAt = email, s.now()
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.users.UpdateUser(ctx, u); err != nil {
			if errors.Is(err, identity.ErrEmailTaken) {
				conflict := shared.NewConflict("email already in use")
				conflict.Details = map[string]any{"email": "taken"}
				return conflict
			}
			return err
		}
		return s.record(ctx, a, u, "user.email_changed", map[string]any{"email": previous}, map[string]any{"email": email}, nil)
	})
	if err != nil {
		return nil, err
	}
	if s.sessions != nil {
		s.sessions.InvalidateUser(u.ID)
	}
	return s.profileOf(ctx, u), nil
}

func (s *Service) Avatar(ctx context.Context, userID uuid.UUID) (*identity.Avatar, error) {
	return s.avatars.Avatar(ctx, userID)
}

func (s *Service) SetAvatar(ctx context.Context, a Actor, data []byte) (*Profile, error) {
	avatar, err := identity.NewAvatar(data)
	if err != nil {
		return nil, err
	}
	u, err := s.load(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	avatar.UpdatedAt = s.now()
	if err := s.avatars.SetAvatar(ctx, u.ID, avatar); err != nil {
		return nil, err
	}
	_ = s.record(ctx, a, u, "user.avatar_updated", nil, nil, map[string]any{"content_type": avatar.ContentType, "bytes": len(avatar.Data)})
	u.AvatarUpdatedAt = &avatar.UpdatedAt
	return s.profileOf(ctx, u), nil
}

func (s *Service) DeleteAvatar(ctx context.Context, a Actor) (*Profile, error) {
	u, err := s.load(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if u.AvatarUpdatedAt == nil {
		return s.profileOf(ctx, u), nil
	}
	if err := s.avatars.DeleteAvatar(ctx, u.ID); err != nil {
		return nil, err
	}
	_ = s.record(ctx, a, u, "user.avatar_removed", nil, nil, nil)
	u.AvatarUpdatedAt = nil
	return s.profileOf(ctx, u), nil
}

func (s *Service) load(ctx context.Context, id uuid.UUID) (*identity.User, error) {
	u, err := s.users.FindUserByID(ctx, id)
	if err != nil || u == nil || !u.IsActive {
		return nil, shared.NewNotFound("user")
	}
	return u, nil
}

// profileOf resolves the team name; a team the caller cannot read is left out.
func (s *Service) profileOf(ctx context.Context, u *identity.User) *Profile {
	p := &Profile{User: *u}
	p.User.PasswordHash = ""
	if u.TeamID != nil {
		if t, err := s.users.FindTeam(ctx, *u.TeamID); err == nil {
			p.Team = t
		}
	}
	return p
}

func (s *Service) record(ctx context.Context, a Actor, u *identity.User, action string, before, after, extra map[string]any) error {
	if s.audit == nil {
		return nil
	}
	id := u.ID
	in := audit.RecordInput{
		ActorID: a.UserID, Action: action, EntityType: "user", EntityID: &id,
		Before: before, After: after, Extra: extra, IP: a.IP, UserAgent: a.UserAgent,
	}
	if u.BranchID != uuid.Nil {
		branch := u.BranchID
		in.BranchID = &branch
	}
	return s.audit.Record(ctx, in)
}

func only(m map[string]any, keys []string) map[string]any {
	out := make(map[string]any, len(keys))
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}
