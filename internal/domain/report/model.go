package report

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Kind identifies a parameterized management report (T-180…T-185).
type Kind string

const (
	KindSales        Kind = "sales"
	KindTargets      Kind = "targets"
	KindReadiness    Kind = "readiness"
	KindSLA          Kind = "sla"
	KindFinance      Kind = "finance"
	KindIntegrations Kind = "integrations"
)

func ValidKind(k Kind) bool {
	switch k {
	case KindSales, KindTargets, KindReadiness, KindSLA, KindFinance, KindIntegrations:
		return true
	default:
		return false
	}
}

// IsSensitive reports require export audit trail (T-188).
func IsSensitive(k Kind) bool {
	return k == KindFinance || k == KindIntegrations || k == KindSales
}

// Filter is the canonical parameterized scope for all reports.
type Filter struct {
	BranchID    uuid.UUID
	From        time.Time
	To          time.Time
	OwnerID     *uuid.UUID
	Channel     string
	Provider    string
	Status      string
	DepartureID *uuid.UUID
	Limit       int
}

func (f Filter) Normalize() (Filter, error) {
	if f.BranchID == uuid.Nil {
		return f, shared.NewValidation("branch_id is required")
	}
	if f.From.IsZero() || f.To.IsZero() {
		now := time.Now().UTC()
		if f.To.IsZero() {
			f.To = now
		}
		if f.From.IsZero() {
			f.From = f.To.AddDate(0, 0, -30)
		}
	}
	if !f.To.After(f.From) {
		return f, shared.NewValidation("to must be after from")
	}
	if f.To.Sub(f.From) > 366*24*time.Hour {
		return f, shared.NewValidation("period cannot exceed 366 days")
	}
	if f.Limit <= 0 {
		f.Limit = 200
	}
	if f.Limit > 1000 {
		f.Limit = 1000
	}
	f.Channel = strings.TrimSpace(f.Channel)
	f.Provider = strings.TrimSpace(f.Provider)
	f.Status = strings.TrimSpace(f.Status)
	return f, nil
}

// DrillRef links a report row to a source record (T-187).
type DrillRef struct {
	EntityType string `json:"entity_type"`
	EntityID   string `json:"entity_id"`
	HrefHint   string `json:"href_hint"`
	Label      string `json:"label,omitempty"`
}

// Row is a generic tabular report line with optional drill-down.
type Row struct {
	ID         string         `json:"id"`
	Label      string         `json:"label"`
	Metrics    map[string]any `json:"metrics"`
	Severity   string         `json:"severity,omitempty"`
	Drilldowns []DrillRef     `json:"drilldowns,omitempty"`
}

// Result is the standard report envelope.
type Result struct {
	Kind        Kind           `json:"kind"`
	GeneratedAt time.Time      `json:"generated_at"`
	Filter      map[string]any `json:"filter"`
	Summary     map[string]any `json:"summary"`
	Rows        []Row          `json:"rows"`
	Columns     []string       `json:"columns"`
}

// IntegrationLog is a durable channel/provider event (T-185).
type IntegrationLog struct {
	ID            uuid.UUID
	BranchID      uuid.UUID
	Provider      string
	Direction     string
	Status        string
	Summary       string
	Detail        string
	CorrelationID string
	EntityType    string
	EntityID      *uuid.UUID
	CreatedAt     time.Time
}

// ExportAudit records sensitive CSV downloads (T-188).
type ExportAudit struct {
	ID         uuid.UUID
	BranchID   uuid.UUID
	ActorID    uuid.UUID
	ReportKind Kind
	Filters    map[string]any
	RowCount   int
	CreatedAt  time.Time
}

// ColumnsFor returns stable CSV/table columns per kind.
func ColumnsFor(k Kind) []string {
	switch k {
	case KindSales:
		return []string{"leads_handled", "leads_won", "conversion_bps", "open_tasks", "overdue_tasks", "collected_amt"}
	case KindTargets:
		return []string{"metric", "target_amount", "actual_amount", "expected_to_date", "variance", "progress_bps", "status", "currency"}
	case KindReadiness:
		return []string{"status", "pax_count", "balance_amt", "missing_docs", "can_confirm", "risk_count"}
	case KindSLA:
		return []string{"channel", "conversations", "breached", "breach_bps", "avg_unanswered_hours", "open_unassigned"}
	case KindFinance:
		return []string{"booked_amt", "collected_amt", "balance_amt", "payment_count", "overdue_count", "currency"}
	case KindIntegrations:
		return []string{"provider", "direction", "status", "summary", "correlation_id", "created_at"}
	default:
		return nil
	}
}

// BuildCSV writes UTF-8 BOM CSV from column headers + rows (pure).
func BuildCSV(columns []string, rows []Row) ([]byte, error) {
	var b strings.Builder
	b.WriteString("\ufeff")
	w := csv.NewWriter(&b)
	header := append([]string{"id", "label"}, columns...)
	header = append(header, "severity", "drill_href")
	if err := w.Write(header); err != nil {
		return nil, err
	}
	for _, r := range rows {
		line := make([]string, 0, len(header))
		line = append(line, r.ID, r.Label)
		for _, c := range columns {
			line = append(line, Stringify(r.Metrics[c]))
		}
		line = append(line, r.Severity)
		href := ""
		if len(r.Drilldowns) > 0 {
			href = r.Drilldowns[0].HrefHint
		}
		line = append(line, href)
		if err := w.Write(line); err != nil {
			return nil, err
		}
	}
	w.Flush()
	return []byte(b.String()), w.Error()
}

// Stringify converts metric values to CSV-safe text.
func Stringify(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case time.Time:
		return t.UTC().Format(time.RFC3339)
	default:
		return fmt.Sprint(t)
	}
}

// FilterMap serializes filter for API/audit.
func FilterMap(f Filter) map[string]any {
	m := map[string]any{
		"branch_id": f.BranchID.String(),
		"from":      f.From.UTC().Format(time.RFC3339),
		"to":        f.To.UTC().Format(time.RFC3339),
		"limit":     f.Limit,
	}
	if f.OwnerID != nil {
		m["owner_id"] = f.OwnerID.String()
	}
	if f.Channel != "" {
		m["channel"] = f.Channel
	}
	if f.Provider != "" {
		m["provider"] = f.Provider
	}
	if f.Status != "" {
		m["status"] = f.Status
	}
	if f.DepartureID != nil {
		m["departure_id"] = f.DepartureID.String()
	}
	return m
}

// Repository aggregates report queries + integration/export persistence (DIP).
type Repository interface {
	SalesRows(ctx context.Context, f Filter) ([]Row, map[string]any, error)
	TargetRows(ctx context.Context, f Filter) ([]Row, map[string]any, error)
	ReadinessRows(ctx context.Context, f Filter) ([]Row, map[string]any, error)
	SLARows(ctx context.Context, f Filter) ([]Row, map[string]any, error)
	FinanceRows(ctx context.Context, f Filter) ([]Row, map[string]any, error)

	ListIntegrationLogs(ctx context.Context, f Filter) ([]IntegrationLog, int, error)
	InsertIntegrationLog(ctx context.Context, l *IntegrationLog) error
	InsertExportAudit(ctx context.Context, a *ExportAudit) error
}
