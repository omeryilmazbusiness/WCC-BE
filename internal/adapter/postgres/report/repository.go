package report

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/report"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) SalesRows(ctx context.Context, f domain.Filter) ([]domain.Row, map[string]any, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		WITH owners AS (
			SELECT DISTINCT owner_id AS id FROM leads
			WHERE created_at >= $1 AND created_at < $2 AND branch_id=$3
			  AND ($4::uuid IS NULL OR owner_id=$4)
			UNION
			SELECT DISTINCT owner_id FROM bookings
			WHERE created_at >= $1 AND created_at < $2 AND branch_id=$3
			  AND ($4::uuid IS NULL OR owner_id=$4)
		)
		SELECT o.id::text,
			COALESCE(u.full_name, o.id::text),
			(SELECT COUNT(*) FROM leads l WHERE l.owner_id=o.id AND l.created_at >= $1 AND l.created_at < $2 AND l.branch_id=$3),
			(SELECT COUNT(*) FROM leads l WHERE l.owner_id=o.id AND l.stage='won' AND l.updated_at >= $1 AND l.updated_at < $2 AND l.branch_id=$3),
			(SELECT COUNT(*) FROM tasks t WHERE t.assignee_id=o.id AND t.status IN ('open','in_progress') AND t.branch_id=$3),
			(SELECT COUNT(*) FROM tasks t WHERE t.assignee_id=o.id AND t.status IN ('open','in_progress') AND t.due_at < NOW() AND t.branch_id=$3),
			(SELECT COALESCE(SUM(b.collected_amt),0) FROM bookings b WHERE b.owner_id=o.id AND b.created_at >= $1 AND b.created_at < $2 AND b.branch_id=$3)
		FROM owners o
		LEFT JOIN users u ON u.id = o.id
		ORDER BY 7 DESC, 3 DESC
		LIMIT $5`, f.From, f.To, f.BranchID, f.OwnerID, f.Limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []domain.Row
	var totalLeads, totalWon int
	var totalCollected int64
	for rows.Next() {
		var id, name string
		var handled, won, openTasks, overdue int
		var collected int64
		if err := rows.Scan(&id, &name, &handled, &won, &openTasks, &overdue, &collected); err != nil {
			return nil, nil, err
		}
		bps := 0
		if handled > 0 {
			bps = (won * 10000) / handled
		}
		sev := "info"
		if overdue > 0 {
			sev = "warning"
		}
		totalLeads += handled
		totalWon += won
		totalCollected += collected
		out = append(out, domain.Row{
			ID: id, Label: name, Severity: sev,
			Metrics: map[string]any{
				"leads_handled": handled, "leads_won": won, "conversion_bps": bps,
				"open_tasks": openTasks, "overdue_tasks": overdue, "collected_amt": collected,
			},
			Drilldowns: []domain.DrillRef{
				{EntityType: "user", EntityID: id, HrefHint: "/pipeline?owner=" + id, Label: "Pipeline"},
				{EntityType: "user", EntityID: id, HrefHint: "/bookings?owner=" + id, Label: "Bookings"},
			},
		})
	}
	conv := 0
	if totalLeads > 0 {
		conv = (totalWon * 10000) / totalLeads
	}
	return out, map[string]any{
		"owners": len(out), "leads_handled": totalLeads, "leads_won": totalWon,
		"conversion_bps": conv, "collected_amt": totalCollected,
	}, rows.Err()
}

func (r *Repository) TargetRows(ctx context.Context, f domain.Filter) ([]domain.Row, map[string]any, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT t.id::text, t.label, COALESCE(t.metric,'collected'), t.target_amount, t.currency,
		       COALESCE(s.actual_amount,0), COALESCE(s.expected_to_date,0),
		       COALESCE(s.progress_bps,0), COALESCE(s.status,'unknown'),
		       t.period_start::text, t.period_end::text
		FROM revenue_targets t
		LEFT JOIN LATERAL (
			SELECT actual_amount, expected_to_date, progress_bps, status
			FROM revenue_target_snapshots
			WHERE target_id = t.id
			ORDER BY as_of DESC
			LIMIT 1
		) s ON TRUE
		WHERE t.branch_id = $1
		  AND t.period_start < ($3::timestamptz)::date
		  AND t.period_end >= ($2::timestamptz)::date
		  AND ($4::uuid IS NULL OR t.owner_id = $4 OR EXISTS (
		        SELECT 1 FROM revenue_target_shares sh WHERE sh.target_id=t.id AND sh.user_id=$4
		      ))
		ORDER BY t.period_start DESC
		LIMIT $5`, f.BranchID, f.From, f.To, f.OwnerID, f.Limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []domain.Row
	var ahead, onTrack, behind int
	for rows.Next() {
		var id, name, metric, currency, status, pStart, pEnd string
		var amount, actual, expected int64
		var progress int
		if err := rows.Scan(&id, &name, &metric, &amount, &currency, &actual, &expected, &progress, &status, &pStart, &pEnd); err != nil {
			return nil, nil, err
		}
		variance := actual - expected
		sev := "info"
		switch status {
		case "behind":
			sev = "critical"
			behind++
		case "on_track":
			sev = "warning"
			onTrack++
		case "ahead":
			ahead++
		}
		out = append(out, domain.Row{
			ID: id, Label: name, Severity: sev,
			Metrics: map[string]any{
				"metric": metric, "target_amount": amount, "actual_amount": actual,
				"expected_to_date": expected, "variance": variance, "progress_bps": progress,
				"status": status, "currency": currency, "period_start": pStart, "period_end": pEnd,
			},
			Drilldowns: []domain.DrillRef{
				{EntityType: "target", EntityID: id, HrefHint: "/targets", Label: "Target board"},
			},
		})
	}
	return out, map[string]any{
		"targets": len(out), "ahead": ahead, "on_track": onTrack, "behind": behind,
	}, rows.Err()
}

func (r *Repository) ReadinessRows(ctx context.Context, f domain.Filter) ([]domain.Row, map[string]any, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT b.id::text,
			COALESCE(c.full_name, b.id::text),
			b.status, b.pax_count, b.balance_amt,
			(SELECT COUNT(*) FROM documents d
			  WHERE d.related_type='booking' AND d.related_id=b.id
			    AND d.status NOT IN ('approved')),
			EXISTS (SELECT 1 FROM booking_readiness_overrides o WHERE o.booking_id=b.id)
		FROM bookings b
		LEFT JOIN customers c ON c.id = b.customer_id
		WHERE b.branch_id=$1
		  AND b.status IN ('draft','confirmed')
		  AND ($2::uuid IS NULL OR b.departure_id=$2)
		  AND b.created_at >= $3 AND b.created_at < $4
		ORDER BY b.balance_amt DESC, b.updated_at DESC
		LIMIT $5`, f.BranchID, f.DepartureID, f.From, f.To, f.Limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []domain.Row
	blocked, ready, overridden := 0, 0, 0
	for rows.Next() {
		var id, label, status string
		var pax, missing int
		var balance int64
		var hasOverride bool
		if err := rows.Scan(&id, &label, &status, &pax, &balance, &missing, &hasOverride); err != nil {
			return nil, nil, err
		}
		canConfirm := missing == 0 && balance <= 0
		if hasOverride {
			canConfirm = true
			overridden++
		}
		sev := "info"
		risk := 0
		if missing > 0 {
			sev = "warning"
			risk++
		}
		if balance > 0 {
			sev = "warning"
			risk++
		}
		if !canConfirm && !hasOverride {
			sev = "critical"
			blocked++
		} else {
			ready++
		}
		out = append(out, domain.Row{
			ID: id, Label: label, Severity: sev,
			Metrics: map[string]any{
				"status": status, "pax_count": pax, "balance_amt": balance,
				"missing_docs": missing, "can_confirm": canConfirm, "risk_count": risk,
				"override_active": hasOverride,
			},
			Drilldowns: []domain.DrillRef{
				{EntityType: "booking", EntityID: id, HrefHint: "/bookings/" + id, Label: "Booking"},
				{EntityType: "booking", EntityID: id, HrefHint: "/missing-docs", Label: "Missing docs"},
			},
		})
	}
	return out, map[string]any{
		"bookings": len(out), "ready": ready, "blocked": blocked, "overrides": overridden,
	}, rows.Err()
}

func (r *Repository) SLARows(ctx context.Context, f domain.Filter) ([]domain.Row, map[string]any, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT channel,
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE sla_breached_at IS NOT NULL)::int,
			COALESCE(AVG(EXTRACT(EPOCH FROM (COALESCE(sla_stopped_at, NOW()) - unanswered_since))/3600.0)
			  FILTER (WHERE unanswered_since IS NOT NULL), 0),
			COUNT(*) FILTER (WHERE owner_id IS NULL AND status='open')::int
		FROM conversations
		WHERE branch_id=$1
		  AND created_at >= $2 AND created_at < $3
		  AND ($4 = '' OR channel = $4)
		GROUP BY channel
		ORDER BY 3 DESC, 2 DESC
		LIMIT $5`, f.BranchID, f.From, f.To, f.Channel, f.Limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []domain.Row
	var totalConv, totalBreach int
	for rows.Next() {
		var channel string
		var conv, breached, unassigned int
		var avgHours float64
		if err := rows.Scan(&channel, &conv, &breached, &avgHours, &unassigned); err != nil {
			return nil, nil, err
		}
		bps := 0
		if conv > 0 {
			bps = (breached * 10000) / conv
		}
		sev := "info"
		if bps >= 2000 {
			sev = "critical"
		} else if bps >= 500 {
			sev = "warning"
		}
		totalConv += conv
		totalBreach += breached
		out = append(out, domain.Row{
			ID: channel, Label: channel, Severity: sev,
			Metrics: map[string]any{
				"channel": channel, "conversations": conv, "breached": breached,
				"breach_bps": bps, "avg_unanswered_hours": round1(avgHours), "open_unassigned": unassigned,
			},
			Drilldowns: []domain.DrillRef{
				{EntityType: "channel", EntityID: channel, HrefHint: "/inbox?channel=" + channel, Label: "Inbox"},
			},
		})
	}
	breachBps := 0
	if totalConv > 0 {
		breachBps = (totalBreach * 10000) / totalConv
	}
	return out, map[string]any{
		"channels": len(out), "conversations": totalConv, "breached": totalBreach, "breach_bps": breachBps,
	}, rows.Err()
}

func (r *Repository) FinanceRows(ctx context.Context, f domain.Filter) ([]domain.Row, map[string]any, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	type currAgg struct {
		currency                   string
		booked, collected, balance int64
		payments, overdue          int
	}
	aggRows, err := q.Query(ctx, `
		SELECT COALESCE(currency,'SAR'),
			COALESCE(SUM(total_amount),0),
			COALESCE(SUM(collected_amt),0),
			COALESCE(SUM(balance_amt),0),
			COUNT(*) FILTER (WHERE balance_amt > 0 AND status='confirmed')
		FROM bookings
		WHERE branch_id=$1 AND created_at >= $2 AND created_at < $3
		GROUP BY COALESCE(currency,'SAR')
		ORDER BY 3 DESC`, f.BranchID, f.From, f.To)
	if err != nil {
		return nil, nil, err
	}
	defer aggRows.Close()
	var aggs []currAgg
	for aggRows.Next() {
		var a currAgg
		if err := aggRows.Scan(&a.currency, &a.booked, &a.collected, &a.balance, &a.overdue); err != nil {
			return nil, nil, err
		}
		aggs = append(aggs, a)
	}
	if err := aggRows.Err(); err != nil {
		return nil, nil, err
	}
	for i := range aggs {
		_ = q.QueryRow(ctx, `
			SELECT COUNT(*) FROM payments p
			JOIN bookings b ON b.id = p.booking_id
			WHERE b.branch_id=$1 AND p.created_at >= $2 AND p.created_at < $3 AND p.currency=$4`,
			f.BranchID, f.From, f.To, aggs[i].currency).Scan(&aggs[i].payments)
	}

	var out []domain.Row
	var bookedTot, collectedTot, balanceTot int64
	var overdueTot int
	for _, a := range aggs {
		bookedTot += a.booked
		collectedTot += a.collected
		balanceTot += a.balance
		overdueTot += a.overdue
		sev := "info"
		if a.overdue > 0 {
			sev = "warning"
		}
		out = append(out, domain.Row{
			ID: a.currency, Label: "Currency " + a.currency, Severity: sev,
			Metrics: map[string]any{
				"booked_amt": a.booked, "collected_amt": a.collected, "balance_amt": a.balance,
				"payment_count": a.payments, "overdue_count": a.overdue, "currency": a.currency,
			},
			Drilldowns: []domain.DrillRef{
				{EntityType: "finance", EntityID: a.currency, HrefHint: "/finance", Label: "Finance queues"},
			},
		})
	}

	bRows, err := q.Query(ctx, `
		SELECT b.id::text, COALESCE(c.full_name, b.id::text), b.balance_amt, b.currency
		FROM bookings b
		LEFT JOIN customers c ON c.id=b.customer_id
		WHERE b.branch_id=$1 AND b.status='confirmed' AND b.balance_amt > 0
		  AND b.created_at >= $2 AND b.created_at < $3
		ORDER BY b.balance_amt DESC
		LIMIT 25`, f.BranchID, f.From, f.To)
	if err == nil {
		defer bRows.Close()
		for bRows.Next() {
			var id, name, cur string
			var bal int64
			if err := bRows.Scan(&id, &name, &bal, &cur); err != nil {
				break
			}
			out = append(out, domain.Row{
				ID: id, Label: name, Severity: "critical",
				Metrics: map[string]any{
					"booked_amt": int64(0), "collected_amt": int64(0), "balance_amt": bal,
					"payment_count": 0, "overdue_count": 1, "currency": cur,
				},
				Drilldowns: []domain.DrillRef{
					{EntityType: "booking", EntityID: id, HrefHint: "/bookings/" + id, Label: "Booking"},
				},
			})
		}
	}
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, map[string]any{
		"booked_amt": bookedTot, "collected_amt": collectedTot, "balance_amt": balanceTot,
		"overdue_count": overdueTot, "currencies": len(aggs),
	}, nil
}

func (r *Repository) ListIntegrationLogs(ctx context.Context, f domain.Filter) ([]domain.IntegrationLog, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := `branch_id=$1 AND created_at >= $2 AND created_at < $3`
	args := []any{f.BranchID, f.From, f.To}
	i := 4
	if f.Provider != "" {
		where += ` AND provider=$` + strconv.Itoa(i)
		args = append(args, f.Provider)
		i++
	}
	if f.Status != "" {
		where += ` AND status=$` + strconv.Itoa(i)
		args = append(args, f.Status)
		i++
	}
	var total int
	if err := q.QueryRow(ctx, `SELECT COUNT(*) FROM integration_logs WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.Limit)
	rows, err := q.Query(ctx, `
		SELECT id, branch_id, provider, direction, status, summary, detail,
		       correlation_id, entity_type, entity_id, created_at
		FROM integration_logs WHERE `+where+`
		ORDER BY created_at DESC
		LIMIT $`+strconv.Itoa(i), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []domain.IntegrationLog
	for rows.Next() {
		var l domain.IntegrationLog
		if err := rows.Scan(
			&l.ID, &l.BranchID, &l.Provider, &l.Direction, &l.Status, &l.Summary, &l.Detail,
			&l.CorrelationID, &l.EntityType, &l.EntityID, &l.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 {
		hRows, herr := q.Query(ctx, `
			SELECT id, provider, status, COALESCE(last_error,''), COALESCE(last_ok_at, created_at)
			FROM integration_accounts WHERE branch_id=$1`, f.BranchID)
		if herr == nil {
			defer hRows.Close()
			for hRows.Next() {
				var id uuid.UUID
				var provider, status, lastErr string
				var at time.Time
				if err := hRows.Scan(&id, &provider, &status, &lastErr, &at); err != nil {
					break
				}
				st := "ok"
				switch status {
				case "down", "disconnected":
					st = "error"
				case "degraded":
					st = "degraded"
				}
				eid := id
				out = append(out, domain.IntegrationLog{
					ID: id, BranchID: f.BranchID, Provider: provider, Direction: "health",
					Status: st, Summary: "Account " + status, Detail: lastErr,
					EntityType: "integration_account", EntityID: &eid, CreatedAt: at,
				})
			}
			total = len(out)
		}
	}
	return out, total, nil
}

func (r *Repository) InsertIntegrationLog(ctx context.Context, l *domain.IntegrationLog) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO integration_logs (
			id, branch_id, provider, direction, status, summary, detail,
			correlation_id, entity_type, entity_id, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		l.ID, l.BranchID, l.Provider, l.Direction, l.Status, l.Summary, l.Detail,
		l.CorrelationID, l.EntityType, l.EntityID, l.CreatedAt,
	)
	return err
}

func (r *Repository) InsertExportAudit(ctx context.Context, a *domain.ExportAudit) error {
	q := tx.QuerierFrom(ctx, r.pool)
	raw, _ := json.Marshal(a.Filters)
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	_, err := q.Exec(ctx, `
		INSERT INTO report_export_audits (id, branch_id, actor_id, report_kind, filters_json, row_count, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		a.ID, a.BranchID, a.ActorID, string(a.ReportKind), raw, a.RowCount, a.CreatedAt,
	)
	return err
}

func round1(v float64) float64 {
	return float64(int(v*10+0.5)) / 10
}
