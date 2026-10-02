package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	pgcompany "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/company"
	pgsetup "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/setup"
	appai "github.com/wodi-crm/wodi-crm-be/internal/app/ai"
	appbranding "github.com/wodi-crm/wodi-crm-be/internal/app/branding"
	appcompany "github.com/wodi-crm/wodi-crm-be/internal/app/company"
	appinbox "github.com/wodi-crm/wodi-crm-be/internal/app/inbox"
	appsetup "github.com/wodi-crm/wodi-crm-be/internal/app/setup"
	appuser "github.com/wodi-crm/wodi-crm-be/internal/app/useradmin"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// workspaceTTL bounds how long another API instance may serve a stale
// tenant (branch list, suspension) after a change it did not see.
const workspaceTTL = 15 * time.Second

type tenancyModule struct {
	companies  *appcompany.Service
	workspaces *appcompany.WorkspaceCache
	setup      *appsetup.Service
	branding   *appbranding.Service
}

func newTenancyModule(pool *pgxpool.Pool, users *appuser.Service, ai *appai.Service, inbox *appinbox.Service, txm tx.Runner, auditRec audit.Recorder) tenancyModule {
	repo := pgcompany.NewRepository(pool)
	cache := appcompany.NewWorkspaceCache(repo, workspaceTTL)
	setupRepo := pgsetup.NewRepository(pool)
	return tenancyModule{
		companies:  appcompany.NewService(repo, gmCreator{users: users}, txm, auditRec, cache),
		workspaces: cache,
		branding:   appbranding.NewService(repo, auditRec),
		setup: appsetup.NewService(appsetup.Deps{
			Repo: setupRepo, Companies: repo, Staff: setupRepo,
			AI: setupAIBridge{svc: ai}, Channels: setupChannelBridge{svc: inbox},
			Tx: txm, Audit: auditRec, Caches: cache,
		}),
	}
}

// gmCreator creates a company's GM through user administration, which owns
// hashing, placement rules and the user audit trail.
type gmCreator struct {
	users *appuser.Service
}

func (g gmCreator) CreateGM(ctx context.Context, branchID uuid.UUID, gm appcompany.GMAccount, actor appcompany.Actor) (uuid.UUID, error) {
	u, err := g.users.Create(ctx, appuser.CreateInput{
		Email: gm.Email, Password: gm.Password, FullName: gm.FullName, Role: platformauth.RoleGM,
		BranchID: branchID, ActorID: actor.UserID, IP: actor.IP, UserAgent: actor.UserAgent,
	})
	if errors.Is(err, shared.ErrConflict) {
		conflict := shared.NewConflict("gm email already in use")
		conflict.Details = map[string]any{"gm_email": "already in use"}
		return uuid.Nil, conflict
	}
	if err != nil {
		return uuid.Nil, err
	}
	return u.ID, nil
}

// setupAIBridge reads readiness from the public AI setup view.
type setupAIBridge struct {
	svc *appai.Service
}

func (b setupAIBridge) AIReady(ctx context.Context, branchID uuid.UUID) (bool, error) {
	st, err := b.svc.GetSetup(ctx, branchID)
	if err != nil {
		return false, err
	}
	configured, _ := st["configured"].(bool)
	enabled, _ := st["enabled"].(bool)
	return configured && enabled, nil
}

// setupChannelBridge counts connected inbox channels.
type setupChannelBridge struct {
	svc *appinbox.Service
}

func (b setupChannelBridge) ConnectedChannels(ctx context.Context, branchID uuid.UUID) (int, error) {
	accounts, err := b.svc.ListAccounts(ctx, branchID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range accounts {
		if a.Connected {
			n++
		}
	}
	return n, nil
}
