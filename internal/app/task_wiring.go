package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	pgidentity "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/identity"
	pgnotification "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/notification"
	pgtask "github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/task"
	appnotification "github.com/wodi-crm/wodi-crm-be/internal/app/notification"
	apptask "github.com/wodi-crm/wodi-crm-be/internal/app/task"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type overdueEscalator interface {
	EscalateOverdue(ctx context.Context, requested *uuid.UUID) (int, error)
}

// TaskJobs runs scheduled task maintenance in the worker.
type TaskJobs struct {
	svc overdueEscalator
	log *slog.Logger
}

// NewTaskJobs wires the task service with the same task.escalated → notification
// reactor as the API; without it scheduled escalations would notify nobody.
func NewTaskJobs(pool *pgxpool.Pool, log *slog.Logger) *TaskJobs {
	txm := tx.NewManager(pool)
	bus := events.NewBus(log)
	notif := appnotification.NewService(pgnotification.NewRepository(pool), txm, log)
	notif.SetUserDirectory(identityDirectory{repo: pgidentity.NewRepository(pool)})
	notif.SetExternal(appnotification.LogExternal{Log: log})
	appnotification.NewReactor(notif).Register(bus)
	return &TaskJobs{svc: apptask.NewService(pgtask.NewRepository(pool), txm, bus), log: log}
}

// EscalateOverdue is the task.escalate_overdue handler (all branches, system actor).
func (j *TaskJobs) EscalateOverdue(ctx context.Context, _ []byte) error {
	n, err := j.svc.EscalateOverdue(audit.AsSystem(access.WithScope(ctx, access.System())), nil)
	if err != nil {
		return err
	}
	if n > 0 {
		j.log.InfoContext(ctx, "overdue tasks escalated", "count", n)
	}
	return nil
}
