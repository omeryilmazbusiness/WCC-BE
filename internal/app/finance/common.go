package finance

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/finance"
)

// Base holds the collaborators every finance service shares.
type Base struct {
	Audit audit.Recorder
	Log   *slog.Logger
	// Location is the business time zone calendar dates are taken in.
	Location *time.Location
	Now      func() time.Time
}

func (b Base) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

func (b Base) today() time.Time { return domain.Day(b.now(), b.Location) }

func (b Base) record(ctx context.Context, actor uuid.UUID, action, entity string, id uuid.UUID, branch uuid.UUID, after any) error {
	if b.Audit == nil {
		return nil
	}
	return b.Audit.Record(ctx, audit.RecordInput{
		ActorID: actor, Action: action, EntityType: entity, EntityID: &id, BranchID: &branch, After: after,
	})
}

func (b Base) logger() *slog.Logger {
	if b.Log != nil {
		return b.Log
	}
	return slog.Default()
}

// Hub groups the finance services for wiring and the HTTP adapter.
type Hub struct {
	Overview    *OverviewService
	Treasury    *TreasuryService
	Receivables *ReceivablesService
	Payables    *PayablesService
	Profit      *ProfitService
	Recon       *ReconService
}
