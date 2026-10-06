package profile

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

type fakeUsers struct {
	byID    map[uuid.UUID]*identity.User
	teams   map[uuid.UUID]*identity.Team
	updates int
}

func (f *fakeUsers) FindUserByID(_ context.Context, id uuid.UUID) (*identity.User, error) {
	if u, ok := f.byID[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, errors.New("user: not found")
}

func (f *fakeUsers) UpdateUser(_ context.Context, u *identity.User) error {
	for id, other := range f.byID {
		if id != u.ID && other.Email == u.Email {
			return identity.ErrEmailTaken
		}
	}
	cp := *u
	f.byID[u.ID] = &cp
	f.updates++
	return nil
}

func (f *fakeUsers) FindTeam(_ context.Context, id uuid.UUID) (*identity.Team, error) {
	if t, ok := f.teams[id]; ok {
		return t, nil
	}
	return nil, errors.New("team: not found")
}

type fakeAvatars struct{ byUser map[uuid.UUID]identity.Avatar }

func (f *fakeAvatars) Avatar(_ context.Context, id uuid.UUID) (*identity.Avatar, error) {
	if a, ok := f.byUser[id]; ok {
		return &a, nil
	}
	return nil, shared.NewNotFound("avatar")
}

func (f *fakeAvatars) SetAvatar(_ context.Context, id uuid.UUID, a identity.Avatar) error {
	f.byUser[id] = a
	return nil
}

func (f *fakeAvatars) DeleteAvatar(_ context.Context, id uuid.UUID) error {
	delete(f.byUser, id)
	return nil
}

type fakeAudit struct{ records []audit.RecordInput }

func (f *fakeAudit) Record(_ context.Context, in audit.RecordInput) error {
	f.records = append(f.records, in)
	return nil
}

type fakeSessions struct{ users []uuid.UUID }

func (f *fakeSessions) InvalidateUser(id uuid.UUID) { f.users = append(f.users, id) }

type harness struct {
	svc      *Service
	users    *fakeUsers
	avatars  *fakeAvatars
	audit    *fakeAudit
	sessions *fakeSessions
	me       *identity.User
	actor    Actor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-horse-1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	team := &identity.Team{ID: uuid.New(), NameEN: "Sales", NameAR: "المبيعات"}
	me := &identity.User{
		ID: uuid.New(), Email: "gm@wodi.example", PasswordHash: string(hash), FullName: "General Manager",
		Role: platformauth.RoleGM, BranchID: uuid.New(), TeamID: &team.ID, IsActive: true,
	}
	other := &identity.User{ID: uuid.New(), Email: "taken@wodi.example", IsActive: true}
	h := &harness{
		users: &fakeUsers{
			byID:  map[uuid.UUID]*identity.User{me.ID: me, other.ID: other},
			teams: map[uuid.UUID]*identity.Team{team.ID: team},
		},
		avatars: &fakeAvatars{byUser: map[uuid.UUID]identity.Avatar{}}, audit: &fakeAudit{}, sessions: &fakeSessions{},
		me: me, actor: Actor{UserID: me.ID, IP: "10.0.0.1"},
	}
	h.svc = NewService(h.users, h.avatars, h.audit, nil, h.sessions)
	return h
}

func ptr(s string) *string { return &s }

func detailOf(t *testing.T, err error, sentinel error) string {
	t.Helper()
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(err, sentinel) {
		t.Fatalf("want %v, got %v", sentinel, err)
	}
	for k, v := range app.Details {
		return k + "=" + v.(string)
	}
	return ""
}

func TestGetHidesPasswordAndResolvesTeam(t *testing.T) {
	h := newHarness(t)
	p, err := h.svc.Get(context.Background(), h.me.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.User.PasswordHash != "" || p.Team == nil || p.Team.NameEN != "Sales" {
		t.Fatalf("profile: %+v", p)
	}
	if _, err := h.svc.Get(context.Background(), uuid.New()); !errors.Is(err, shared.ErrNotFound) {
		t.Fatal("unknown user is not found")
	}
}

func TestUpdateChangesOnlyGivenFieldsAndAudits(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p, err := h.svc.Update(ctx, h.actor, UpdateInput{FullName: ptr("  Ömer   Yılmaz "), Phone: ptr("+90 532 123 45 67")})
	if err != nil {
		t.Fatal(err)
	}
	if p.User.FullName != "Ömer Yılmaz" || p.User.Phone != "+90 532 123 45 67" || p.User.Email != h.me.Email {
		t.Fatalf("updated: %+v", p.User)
	}
	rec := h.audit.records[len(h.audit.records)-1]
	after, _ := rec.After.(map[string]any)
	before, _ := rec.Before.(map[string]any)
	if rec.Action != "user.profile_updated" || len(after) != 2 || before["full_name"] != "General Manager" {
		t.Fatalf("audit must list only the changed fields: %+v", rec)
	}

	updates := h.users.updates
	if _, err := h.svc.Update(ctx, h.actor, UpdateInput{FullName: ptr("Ömer Yılmaz")}); err != nil || h.users.updates != updates {
		t.Fatal("an unchanged value must not write")
	}
	if _, err := h.svc.Update(ctx, h.actor, UpdateInput{Phone: ptr("")}); err != nil || h.users.byID[h.me.ID].Phone != "" {
		t.Fatal("empty phone clears it")
	}
	if got := detailOf(t, mustFail(h.svc.Update(ctx, h.actor, UpdateInput{Phone: ptr("12")})), shared.ErrValidation); got != "phone=invalid" {
		t.Fatalf("phone: %s", got)
	}
	if got := detailOf(t, mustFail(h.svc.Update(ctx, h.actor, UpdateInput{FullName: ptr(" ")})), shared.ErrValidation); got != "full_name=required" {
		t.Fatalf("name: %s", got)
	}
}

func TestChangeEmailNeedsPasswordAndAFreeAddress(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	change := func(email, password string) (*Profile, error) {
		return h.svc.ChangeEmail(ctx, h.actor, ChangeEmailInput{Email: email, Password: password})
	}
	if got := detailOf(t, mustFail(change("new@wodi.example", "wrong")), shared.ErrValidation); got != "password=incorrect" {
		t.Fatalf("wrong password: %s", got)
	}
	if got := detailOf(t, mustFail(change("not-an-email", "correct-horse-1")), shared.ErrValidation); got != "email=invalid" {
		t.Fatalf("invalid: %s", got)
	}
	if got := detailOf(t, mustFail(change(" GM@wodi.example ", "correct-horse-1")), shared.ErrValidation); got != "email=unchanged" {
		t.Fatalf("unchanged: %s", got)
	}
	if got := detailOf(t, mustFail(change("taken@wodi.example", "correct-horse-1")), shared.ErrConflict); got != "email=taken" {
		t.Fatalf("taken: %s", got)
	}
	p, err := change("New.Address@Wodi.Example", "correct-horse-1")
	if err != nil || p.User.Email != "new.address@wodi.example" {
		t.Fatalf("change: %v %+v", err, p)
	}
	rec := h.audit.records[len(h.audit.records)-1]
	if previous, _ := rec.Before.(map[string]any); rec.Action != "user.email_changed" || previous["email"] != "gm@wodi.example" {
		t.Fatalf("audit: %+v", rec)
	}
	if len(h.sessions.users) != 1 {
		t.Fatal("cached token checks must be dropped: tokens carry the email")
	}
}

func TestAvatarLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)
	p, err := h.svc.SetAvatar(ctx, h.actor, png)
	if err != nil || p.User.AvatarUpdatedAt == nil {
		t.Fatalf("set: %v", err)
	}
	if a, err := h.svc.Avatar(ctx, h.me.ID); err != nil || a.ContentType != "image/png" {
		t.Fatalf("get: %v", err)
	}
	if _, err := h.svc.SetAvatar(ctx, h.actor, []byte("<svg/>")); !errors.Is(err, shared.ErrValidation) {
		t.Fatal("non-raster upload must be refused")
	}
	h.users.byID[h.me.ID].AvatarUpdatedAt = p.User.AvatarUpdatedAt
	p, err = h.svc.DeleteAvatar(ctx, h.actor)
	if err != nil || p.User.AvatarUpdatedAt != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := h.svc.Avatar(ctx, h.me.ID); !errors.Is(err, shared.ErrNotFound) {
		t.Fatal("deleted photo is gone")
	}
	actions := []string{}
	for _, r := range h.audit.records {
		actions = append(actions, r.Action)
	}
	if len(actions) != 2 || actions[0] != "user.avatar_updated" || actions[1] != "user.avatar_removed" {
		t.Fatalf("audit: %v", actions)
	}
}

func TestInactiveUserCannotEdit(t *testing.T) {
	h := newHarness(t)
	h.users.byID[h.me.ID].IsActive = false
	if _, err := h.svc.Update(context.Background(), h.actor, UpdateInput{FullName: ptr("X Y")}); !errors.Is(err, shared.ErrNotFound) {
		t.Fatal("deactivated accounts cannot edit")
	}
}

func mustFail(_ *Profile, err error) error { return err }
