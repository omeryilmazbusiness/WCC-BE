package preference

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/preference"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type memRepo map[uuid.UUID]domain.Preferences

func (m memRepo) Get(_ context.Context, id uuid.UUID) (*domain.Preferences, error) {
	p, ok := m[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (m memRepo) Upsert(_ context.Context, p *domain.Preferences) error {
	cur := m[p.UserID]
	cur.UserID, cur.NavFavorites, cur.UpdatedAt = p.UserID, p.NavFavorites, p.UpdatedAt
	m[p.UserID] = cur
	return nil
}

func (m memRepo) MarkWelcomeSeen(_ context.Context, id uuid.UUID, at time.Time) error {
	cur := m[id]
	cur.UserID = id
	if cur.WelcomeSeenAt == nil {
		cur.WelcomeSeenAt = &at
	}
	cur.UpdatedAt = at
	m[id] = cur
	return nil
}

func TestWelcomeSeen(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()
	svc := NewService(memRepo{})
	first := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	svc.now = func() time.Time { return first }

	p, err := svc.Get(ctx, user)
	if err != nil || p.WelcomeSeenAt != nil {
		t.Fatalf("new user must not have seen the welcome: %+v %v", p, err)
	}

	p, err = svc.MarkWelcomeSeen(ctx, user)
	if err != nil || p.WelcomeSeenAt == nil || !p.WelcomeSeenAt.Equal(first) {
		t.Fatalf("mark: %+v %v", p, err)
	}

	svc.now = func() time.Time { return first.Add(time.Hour) }
	if p, err = svc.MarkWelcomeSeen(ctx, user); err != nil || !p.WelcomeSeenAt.Equal(first) {
		t.Fatalf("repeat must keep the first time: %+v %v", p, err)
	}

	p, err = svc.SetNavFavorites(ctx, user, []string{"/inbox"})
	if err != nil || p.WelcomeSeenAt == nil || len(p.NavFavorites) != 1 {
		t.Fatalf("favorites must not reset the welcome: %+v %v", p, err)
	}
}

func TestNavFavorites(t *testing.T) {
	ctx := context.Background()
	user := uuid.New()
	svc := NewService(memRepo{})

	p, err := svc.Get(ctx, user)
	if err != nil || p.NavFavorites != nil {
		t.Fatalf("default: %+v %v", p, err)
	}

	p, err = svc.SetNavFavorites(ctx, user, []string{" /inbox", "/finance/fx", "/inbox"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.NavFavorites; len(got) != 2 || got[0] != "/inbox" || got[1] != "/finance/fx" {
		t.Fatalf("normalized: %v", got)
	}

	p, err = svc.SetNavFavorites(ctx, user, []string{})
	if err != nil || p.NavFavorites == nil || len(p.NavFavorites) != 0 {
		t.Fatalf("empty list must stay empty, not default: %+v %v", p, err)
	}

	p, err = svc.SetNavFavorites(ctx, user, nil)
	if err != nil || p.NavFavorites != nil {
		t.Fatalf("nil resets to default: %+v %v", p, err)
	}

	for name, in := range map[string][]string{
		"too many": {"/a", "/b", "/c", "/d", "/e", "/f"},
		"bad path": {"inbox"},
		"injected": {"/inbox?x=1"},
		"upper":    {"/Inbox"},
	} {
		if _, err := svc.SetNavFavorites(ctx, user, in); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
}
