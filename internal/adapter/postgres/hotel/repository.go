// Package hotel persists the hotels & contracting module.
package hotel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/postgres/pgscope"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/hotel"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

var _ domain.Repository = (*Repository)(nil)

var hotelScope = pgscope.Columns{Branch: "h.branch_id"}

const hotelColumns = `h.id, h.branch_id, h.name, h.name_ar, h.stars, h.city, h.country, h.district,
	h.latitude, h.longitude, h.landmark, h.distance_m, h.sales_name, h.sales_phone, h.sales_email,
	h.reservations_email, h.room_types, h.meal_plans, h.currency, h.markup, h.child_policy,
	h.cancellation, h.notes, h.is_active, h.created_at, h.updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanHotel(row rowScanner, extra ...any) (*domain.Hotel, error) {
	var h domain.Hotel
	var markup, child, cancel []byte
	dest := []any{
		&h.ID, &h.BranchID, &h.Name, &h.NameAr, &h.Stars, &h.Location.City, &h.Location.Country, &h.Location.District,
		&h.Location.Latitude, &h.Location.Longitude, &h.Location.Landmark, &h.Location.DistanceM,
		&h.Contact.SalesName, &h.Contact.SalesPhone, &h.Contact.SalesEmail, &h.Contact.ReservationsEmail,
		&h.RoomTypes, &h.MealPlans, &h.Currency, &markup, &child, &cancel, &h.Notes, &h.IsActive, &h.CreatedAt, &h.UpdatedAt,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	if err := unmarshal(markup, &h.Markup); err != nil {
		return nil, err
	}
	if err := unmarshal(child, &h.ChildPolicy); err != nil {
		return nil, err
	}
	if err := unmarshal(cancel, &h.Cancellation); err != nil {
		return nil, err
	}
	return &h, nil
}

func unmarshal(raw []byte, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func hotelArgs(h *domain.Hotel) ([]any, error) {
	markup, err := json.Marshal(h.Markup)
	if err != nil {
		return nil, err
	}
	child, err := json.Marshal(h.ChildPolicy)
	if err != nil {
		return nil, err
	}
	cancel, err := json.Marshal(h.Cancellation)
	if err != nil {
		return nil, err
	}
	l, c := h.Location, h.Contact
	return []any{
		h.ID, h.Name, h.NameAr, h.Stars, l.City, l.Country, l.District, l.Latitude, l.Longitude, l.Landmark, l.DistanceM,
		c.SalesName, c.SalesPhone, c.SalesEmail, c.ReservationsEmail, h.RoomTypes, h.MealPlans, h.Currency,
		markup, child, cancel, h.Notes, h.IsActive, h.UpdatedAt,
	}, nil
}

func (r *Repository) Create(ctx context.Context, h *domain.Hotel) error {
	if err := pgscope.EnsureBranch(ctx, h.BranchID); err != nil {
		return err
	}
	args, err := hotelArgs(h)
	if err != nil {
		return err
	}
	_, err = tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO hotels (id, name, name_ar, stars, city, country, district, latitude, longitude, landmark, distance_m,
			sales_name, sales_phone, sales_email, reservations_email, room_types, meal_plans, currency,
			markup, child_policy, cancellation, notes, is_active, updated_at, branch_id, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26)`,
		append(args, h.BranchID, h.CreatedAt)...)
	return err
}

func (r *Repository) Update(ctx context.Context, h *domain.Hotel) error {
	args, err := hotelArgs(h)
	if err != nil {
		return err
	}
	clause, args, err := pgscope.Clause(ctx, pgscope.Columns{Branch: "branch_id"}, args)
	if err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		UPDATE hotels SET name=$2, name_ar=$3, stars=$4, city=$5, country=$6, district=$7, latitude=$8, longitude=$9,
			landmark=$10, distance_m=$11, sales_name=$12, sales_phone=$13, sales_email=$14, reservations_email=$15,
			room_types=$16, meal_plans=$17, currency=$18, markup=$19, child_policy=$20, cancellation=$21, notes=$22,
			is_active=$23, updated_at=$24
		WHERE id=$1`+clause, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound("hotel")
	}
	return nil
}

func (r *Repository) find(ctx context.Context, id uuid.UUID, lock string) (*domain.Hotel, error) {
	clause, args, err := pgscope.Clause(ctx, hotelScope, []any{id})
	if err != nil {
		return nil, err
	}
	h, err := scanHotel(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx,
		`SELECT `+hotelColumns+` FROM hotels h WHERE h.id=$1`+clause+lock, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("hotel")
	}
	return h, err
}

func (r *Repository) Find(ctx context.Context, id uuid.UUID) (*domain.Hotel, error) {
	return r.find(ctx, id, "")
}

func (r *Repository) FindForUpdate(ctx context.Context, id uuid.UUID) (*domain.Hotel, error) {
	return r.find(ctx, id, " FOR UPDATE")
}

func (r *Repository) List(ctx context.Context, f domain.ListFilter) ([]domain.Summary, error) {
	args := []any{f.BranchID, domain.FormatDay(f.Today)}
	where := []string{"h.branch_id=$1"}
	if f.ActiveOnly {
		where = append(where, "h.is_active")
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(h.name ILIKE $%d OR h.name_ar ILIKE $%d OR h.city ILIKE $%d OR h.district ILIKE $%d OR h.sales_name ILIKE $%d)", n, n, n, n, n))
	}
	if c := strings.TrimSpace(f.City); c != "" {
		args = append(args, strings.ToLower(c))
		where = append(where, fmt.Sprintf("lower(h.city)=$%d", len(args)))
	}
	where, args, err := pgscope.Append(ctx, hotelScope, where, args)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT `+hotelColumns+`,
			COALESCE(s.name, ''), COALESCE(s.kind, ''), COALESCE(s.from_net, 0),
			COALESCE(a.rooms, 0), COALESCE(a.sold, 0), a.next_release,
			EXISTS (SELECT 1 FROM hotel_stop_sales ss WHERE ss.hotel_id=h.id AND $2::date BETWEEN ss.start_date AND ss.end_date),
			(SELECT COUNT(*) FROM documents d WHERE d.related_type='hotel' AND d.related_id=h.id),
			(SELECT COUNT(*) FROM hotel_seasons x WHERE x.hotel_id=h.id AND x.end_date >= $2::date),
			COALESCE(a.blocks, 0)
		FROM hotels h
		LEFT JOIN LATERAL (
			SELECT hs.name, hs.kind,
				(SELECT MIN(COALESCE(NULLIF((r->>'double')::bigint, 0), NULLIF((r->>'single')::bigint, 0)))
				 FROM jsonb_array_elements(hs.rates) r) AS from_net
			FROM hotel_seasons hs
			WHERE hs.hotel_id=h.id AND $2::date BETWEEN hs.start_date AND hs.end_date
			ORDER BY hs.start_date LIMIT 1
		) s ON TRUE
		LEFT JOIN LATERAL (
			SELECT SUM(ha.rooms) AS rooms, SUM(ha.sold) AS sold, COUNT(*) AS blocks,
				MIN(ha.start_date - ha.release_days) FILTER (
					WHERE ha.kind='guaranteed' AND ha.sold < ha.rooms AND ha.start_date - ha.release_days >= $2::date
				) AS next_release
			FROM hotel_allotments ha
			WHERE ha.hotel_id=h.id AND ha.end_date >= $2::date
		) a ON TRUE
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY h.is_active DESC, lower(h.city), h.stars DESC, lower(h.name)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Summary{}
	for rows.Next() {
		var s domain.Summary
		var nextRelease *time.Time
		h, err := scanHotel(rows, &s.SeasonName, &s.SeasonKind, &s.FromNet, &s.RoomsTotal, &s.RoomsSold, &nextRelease,
			&s.StopSaleToday, &s.ContractFiles, &s.SeasonsCount, &s.AllotmentCount)
		if err != nil {
			return nil, err
		}
		s.Hotel = *h
		s.NextRelease = nextRelease
		out = append(out, s)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// requireHotel scopes child-table access through the parent hotel.
func (r *Repository) requireHotel(ctx context.Context, hotelID uuid.UUID) error {
	clause, args, err := pgscope.Clause(ctx, hotelScope, []any{hotelID})
	if err != nil {
		return err
	}
	var ok bool
	err = tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `SELECT TRUE FROM hotels h WHERE h.id=$1`+clause, args...).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NewNotFound("hotel")
	}
	return err
}

func (r *Repository) ListSeasons(ctx context.Context, hotelID uuid.UUID) ([]domain.Season, error) {
	if err := r.requireHotel(ctx, hotelID); err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT id, hotel_id, name, kind, start_date, end_date, markup, rates, created_at, updated_at
		FROM hotel_seasons WHERE hotel_id=$1 ORDER BY start_date`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Season{}
	for rows.Next() {
		var s domain.Season
		var markup, rates []byte
		if err := rows.Scan(&s.ID, &s.HotelID, &s.Name, &s.Kind, &s.Start, &s.End, &markup, &rates, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		if len(markup) > 0 && string(markup) != "null" {
			s.Markup = &domain.Markup{}
			if err := json.Unmarshal(markup, s.Markup); err != nil {
				return nil, err
			}
		}
		if err := unmarshal(rates, &s.Rates); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) SaveSeason(ctx context.Context, s *domain.Season) error {
	if err := r.requireHotel(ctx, s.HotelID); err != nil {
		return err
	}
	var markup []byte
	if s.Markup != nil {
		b, err := json.Marshal(s.Markup)
		if err != nil {
			return err
		}
		markup = b
	}
	rates, err := json.Marshal(s.Rates)
	if err != nil {
		return err
	}
	_, err = tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO hotel_seasons (id, hotel_id, name, kind, start_date, end_date, markup, rates, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, kind=EXCLUDED.kind, start_date=EXCLUDED.start_date,
			end_date=EXCLUDED.end_date, markup=EXCLUDED.markup, rates=EXCLUDED.rates, updated_at=EXCLUDED.updated_at
		WHERE hotel_seasons.hotel_id=EXCLUDED.hotel_id`,
		s.ID, s.HotelID, s.Name, s.Kind, s.Start, s.End, markup, rates, s.CreatedAt, s.UpdatedAt)
	return err
}

func (r *Repository) deleteChild(ctx context.Context, table, entity string, hotelID, id uuid.UUID) error {
	if err := r.requireHotel(ctx, hotelID); err != nil {
		return err
	}
	tag, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `DELETE FROM `+table+` WHERE id=$1 AND hotel_id=$2`, id, hotelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return shared.NewNotFound(entity)
	}
	return nil
}

func (r *Repository) DeleteSeason(ctx context.Context, hotelID, id uuid.UUID) error {
	return r.deleteChild(ctx, "hotel_seasons", "season", hotelID, id)
}

const allotmentColumns = `id, hotel_id, room_type, kind, start_date, end_date, rooms, sold, release_days, notes, created_at, updated_at`

func scanAllotment(row rowScanner) (*domain.Allotment, error) {
	var a domain.Allotment
	err := row.Scan(&a.ID, &a.HotelID, &a.RoomType, &a.Kind, &a.Start, &a.End, &a.Rooms, &a.Sold, &a.ReleaseDays, &a.Notes, &a.CreatedAt, &a.UpdatedAt)
	return &a, err
}

func (r *Repository) ListAllotments(ctx context.Context, hotelID uuid.UUID) ([]domain.Allotment, error) {
	if err := r.requireHotel(ctx, hotelID); err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `SELECT `+allotmentColumns+`
		FROM hotel_allotments WHERE hotel_id=$1 ORDER BY start_date, room_type`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Allotment{}
	for rows.Next() {
		a, err := scanAllotment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *Repository) FindAllotment(ctx context.Context, hotelID, id uuid.UUID) (*domain.Allotment, error) {
	if err := r.requireHotel(ctx, hotelID); err != nil {
		return nil, err
	}
	a, err := scanAllotment(tx.QuerierFrom(ctx, r.pool).QueryRow(ctx, `SELECT `+allotmentColumns+`
		FROM hotel_allotments WHERE id=$1 AND hotel_id=$2 FOR UPDATE`, id, hotelID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NewNotFound("allotment")
	}
	return a, err
}

func (r *Repository) SaveAllotment(ctx context.Context, a *domain.Allotment) error {
	if err := r.requireHotel(ctx, a.HotelID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO hotel_allotments (`+allotmentColumns+`)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET room_type=EXCLUDED.room_type, kind=EXCLUDED.kind, start_date=EXCLUDED.start_date,
			end_date=EXCLUDED.end_date, rooms=EXCLUDED.rooms, sold=EXCLUDED.sold, release_days=EXCLUDED.release_days,
			notes=EXCLUDED.notes, updated_at=EXCLUDED.updated_at
		WHERE hotel_allotments.hotel_id=EXCLUDED.hotel_id`,
		a.ID, a.HotelID, a.RoomType, a.Kind, a.Start, a.End, a.Rooms, a.Sold, a.ReleaseDays, a.Notes, a.CreatedAt, a.UpdatedAt)
	return err
}

func (r *Repository) DeleteAllotment(ctx context.Context, hotelID, id uuid.UUID) error {
	return r.deleteChild(ctx, "hotel_allotments", "allotment", hotelID, id)
}

func (r *Repository) ListStopSales(ctx context.Context, hotelID uuid.UUID) ([]domain.StopSale, error) {
	if err := r.requireHotel(ctx, hotelID); err != nil {
		return nil, err
	}
	rows, err := tx.QuerierFrom(ctx, r.pool).Query(ctx, `
		SELECT id, hotel_id, start_date, end_date, room_type, reason, created_by, created_at
		FROM hotel_stop_sales WHERE hotel_id=$1 ORDER BY start_date`, hotelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.StopSale{}
	for rows.Next() {
		var s domain.StopSale
		if err := rows.Scan(&s.ID, &s.HotelID, &s.Start, &s.End, &s.RoomType, &s.Reason, &s.CreatedBy, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repository) CreateStopSale(ctx context.Context, s *domain.StopSale) error {
	if err := r.requireHotel(ctx, s.HotelID); err != nil {
		return err
	}
	_, err := tx.QuerierFrom(ctx, r.pool).Exec(ctx, `
		INSERT INTO hotel_stop_sales (id, hotel_id, start_date, end_date, room_type, reason, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		s.ID, s.HotelID, s.Start, s.End, s.RoomType, s.Reason, s.CreatedBy, s.CreatedAt)
	return err
}

func (r *Repository) DeleteStopSale(ctx context.Context, hotelID, id uuid.UUID) error {
	return r.deleteChild(ctx, "hotel_stop_sales", "stop sale", hotelID, id)
}
