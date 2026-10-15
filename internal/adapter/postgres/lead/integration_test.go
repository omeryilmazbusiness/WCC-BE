package lead

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

var errRollback = errors.New("rollback")

// TestLeadArchiveSearchAgainstPostgres seeds a year of leads inside a
// rolled-back transaction and checks every archive filter over the real
// adapter. It needs a migrated database with at least one active user.
func TestLeadArchiveSearchAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("LEAD_IT_DATABASE_URL")
	if dsn == "" {
		t.Skip("LEAD_IT_DATABASE_URL not set")
	}
	ctx := access.WithScope(context.Background(), access.System())
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewRepository(pool)

	err = tx.NewManager(pool).WithinTransaction(ctx, func(ctx context.Context) error {
		q := tx.QuerierFrom(ctx, pool)
		branch := uuid.New()
		if _, err := q.Exec(ctx, `
			INSERT INTO branches (id, company_id, name_en, code, slug)
			SELECT $1, company_id, 'Archive IT', 'AIT-' || k, 'archive-it-' || k
			FROM branches, substr(md5(random()::text), 1, 8) AS k LIMIT 1`, branch); err != nil {
			return fmt.Errorf("seed branch: %w", err)
		}
		var owner uuid.UUID
		var ownerName string
		if err := q.QueryRow(ctx, `SELECT id, full_name FROM users WHERE full_name <> '' ORDER BY created_at LIMIT 1`).Scan(&owner, &ownerName); err != nil {
			return fmt.Errorf("pick owner: %w", err)
		}

		now := time.Now().UTC().Truncate(time.Second)
		mk := func(name, phone, email, company string, stage domain.Stage, seg domain.Segment, age time.Duration, lost string) uuid.UUID {
			at := now.Add(-age)
			l := &domain.Lead{
				ID: uuid.New(), BranchID: branch, FullName: name, Phone: phone, Source: "instagram",
				Stage: stage, OwnerID: owner, LostReasonCode: lost, CreatedAt: at, UpdatedAt: at,
				Profile: domain.Profile{Email: email, Segment: seg, CompanyName: company, Priority: domain.PriorityMedium},
			}
			if err := repo.Create(ctx, l); err != nil {
				t.Errorf("create %s: %v", name, err)
			}
			return l.ID
		}
		day := 24 * time.Hour
		ayse := mk("Ayşe Kaya", "+90 (555) 123 45 67", "ayse@example.com", "", domain.StageNew, domain.SegmentB2C, 2*day, "")
		acme := mk("Mehmet Demir", "05321112233", "m@acme.io", "Acme 50%_Travel", domain.StageProposal, domain.SegmentB2B, 200*day, "")
		old := mk("Ayşe Yıldız", "05000000000", "", "", domain.StageLost, domain.SegmentB2C, 360*day, domain.LostPrice)
		outside := mk("Ayşe Eski", "05000000001", "", "", domain.StageLost, domain.SegmentB2C, 400*day, domain.LostTiming)
		for i := 0; i < 5; i++ {
			mk("Bulk Lead", "0500", "", "", domain.StageContacted, domain.SegmentB2C, time.Duration(i+1)*time.Hour, "")
		}

		if t.Failed() {
			return errRollback
		}

		yearFrom := domain.RetentionStart(now)
		base := domain.ListFilter{BranchID: &branch, Limit: 100}
		ids := func(f domain.ListFilter) map[uuid.UUID]bool {
			t.Helper()
			items, total, err := repo.List(ctx, f)
			if err != nil {
				t.Errorf("list %+v: %v", f, err)
			}
			if total != len(items) {
				t.Errorf("total %d != items %d", total, len(items))
			}
			out := map[uuid.UUID]bool{}
			for _, it := range items {
				out[it.ID] = true
			}
			return out
		}
		expect := func(name string, f domain.ListFilter, want ...uuid.UUID) {
			t.Helper()
			got := ids(f)
			if len(got) != len(want) {
				t.Errorf("%s: got %d leads, want %d", name, len(got), len(want))
			}
			for _, id := range want {
				if !got[id] {
					t.Errorf("%s: missing %s", name, id)
				}
			}
		}
		with := func(mut func(*domain.ListFilter)) domain.ListFilter {
			f := base
			mut(&f)
			return f
		}

		expect("all tokens, any order", with(func(f *domain.ListFilter) { f.Query = "KAYA ayşe" }), ayse)
		expect("token prefix", with(func(f *domain.ListFilter) { f.Query = "ayş" }), ayse, old, outside)
		expect("year window", with(func(f *domain.ListFilter) { f.Query = "ayşe"; f.CreatedFrom = &yearFrom }), ayse, old)
		expect("phone formatted differently", with(func(f *domain.ListFilter) { f.Query = "555 123-45" }), ayse)
		expect("phone fragment", with(func(f *domain.ListFilter) { f.Query = "1112233" }), acme)
		expect("email", with(func(f *domain.ListFilter) { f.Query = "acme.io" }), acme)
		expect("wildcards are literal", with(func(f *domain.ListFilter) { f.Query = "50%_t" }), acme)
		expect("wildcard never matches all", with(func(f *domain.ListFilter) { f.Query = "%" }), acme)
		expect("no match", with(func(f *domain.ListFilter) { f.Query = "zzqqxx" }))
		expect("lost reason", with(func(f *domain.ListFilter) { f.LostReason = domain.LostPrice }), old)
		expect("segment", with(func(f *domain.ListFilter) { f.Segment = domain.SegmentB2B }), acme)
		if got := ids(with(func(f *domain.ListFilter) { f.Query = ownerName })); len(got) != 9 {
			t.Errorf("owner name search: got %d, want all 9", len(got))
		}

		board, err := repo.Board(ctx, with(func(f *domain.ListFilter) { f.CreatedFrom = &yearFrom }), 3)
		if err != nil {
			return fmt.Errorf("board: %w", err)
		}
		for _, c := range board.Columns {
			want := map[domain.Stage]int{domain.StageNew: 1, domain.StageContacted: 5, domain.StageProposal: 1, domain.StageLost: 1}[c.Stage]
			if c.Total != want {
				t.Errorf("board %s total %d, want %d", c.Stage, c.Total, want)
			}
			if n := min(want, 3); len(c.Items) != n {
				t.Errorf("board %s items %d, want %d", c.Stage, len(c.Items), n)
			}
			for i := 1; i < len(c.Items); i++ {
				if c.Items[i].UpdatedAt.After(c.Items[i-1].UpdatedAt) {
					t.Errorf("board %s not newest first", c.Stage)
				}
			}
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatal(err)
	}
}
