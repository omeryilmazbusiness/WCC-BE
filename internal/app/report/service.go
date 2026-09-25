package report

import (
	"context"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/report"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type Service struct {
	repo domain.Repository
}

func NewService(repo domain.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Run(ctx context.Context, kind domain.Kind, f domain.Filter) (*domain.Result, error) {
	if !domain.ValidKind(kind) {
		return nil, shared.NewValidation("invalid report kind")
	}
	f, err := f.Normalize()
	if err != nil {
		return nil, err
	}
	var rows []domain.Row
	var summary map[string]any
	switch kind {
	case domain.KindSales:
		rows, summary, err = s.repo.SalesRows(ctx, f)
	case domain.KindTargets:
		rows, summary, err = s.repo.TargetRows(ctx, f)
	case domain.KindReadiness:
		rows, summary, err = s.repo.ReadinessRows(ctx, f)
	case domain.KindSLA:
		rows, summary, err = s.repo.SLARows(ctx, f)
	case domain.KindFinance:
		rows, summary, err = s.repo.FinanceRows(ctx, f)
	case domain.KindIntegrations:
		logs, total, lerr := s.repo.ListIntegrationLogs(ctx, f)
		if lerr != nil {
			return nil, lerr
		}
		rows = make([]domain.Row, 0, len(logs))
		errors := 0
		for _, l := range logs {
			if l.Status == "error" || l.Status == "degraded" {
				errors++
			}
			sev := "info"
			if l.Status == "error" {
				sev = "critical"
			} else if l.Status == "degraded" || l.Status == "retry" {
				sev = "warning"
			}
			eid := ""
			if l.EntityID != nil {
				eid = l.EntityID.String()
			}
			rows = append(rows, domain.Row{
				ID: l.ID.String(), Label: l.Provider + " · " + l.Summary, Severity: sev,
				Metrics: map[string]any{
					"provider": l.Provider, "direction": l.Direction, "status": l.Status,
					"summary": l.Summary, "correlation_id": l.CorrelationID,
					"created_at": l.CreatedAt.UTC().Format(time.RFC3339),
					"detail":     l.Detail,
				},
				Drilldowns: []domain.DrillRef{
					{EntityType: l.EntityType, EntityID: eid, HrefHint: "/inbox", Label: "Inbox / integrations"},
				},
			})
		}
		summary = map[string]any{"total": total, "errors": errors, "rows": len(rows)}
	}
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.Row{}
	}
	if summary == nil {
		summary = map[string]any{}
	}
	return &domain.Result{
		Kind: kind, GeneratedAt: time.Now().UTC(),
		Filter: domain.FilterMap(f), Summary: summary, Rows: rows,
		Columns: domain.ColumnsFor(kind),
	}, nil
}

func (s *Service) ExportCSV(ctx context.Context, kind domain.Kind, f domain.Filter, actorID uuid.UUID) ([]byte, string, error) {
	res, err := s.Run(ctx, kind, f)
	if err != nil {
		return nil, "", err
	}
	csv, err := domain.BuildCSV(res.Columns, res.Rows)
	if err != nil {
		return nil, "", err
	}
	if domain.IsSensitive(kind) && actorID != uuid.Nil {
		_ = s.repo.InsertExportAudit(ctx, &domain.ExportAudit{
			ID: uuid.New(), BranchID: f.BranchID, ActorID: actorID,
			ReportKind: kind, Filters: res.Filter, RowCount: len(res.Rows),
			CreatedAt: time.Now().UTC(),
		})
	}
	filename := "report-" + string(kind) + "-" + time.Now().UTC().Format("20060102") + ".csv"
	return csv, filename, nil
}

// RecordIntegrationLog is used by inbox/webhook adapters (ISP port).
func (s *Service) RecordIntegrationLog(ctx context.Context, l *domain.IntegrationLog) error {
	if l == nil || l.BranchID == uuid.Nil {
		return shared.NewValidation("branch_id required")
	}
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	if l.CreatedAt.IsZero() {
		l.CreatedAt = time.Now().UTC()
	}
	if l.Direction == "" {
		l.Direction = "system"
	}
	if l.Status == "" {
		l.Status = "ok"
	}
	return s.repo.InsertIntegrationLog(ctx, l)
}
