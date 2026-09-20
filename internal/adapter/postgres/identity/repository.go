package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/identity"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) FindUserByEmail(ctx context.Context, email string) (*identity.User, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, email, password_hash, full_name, role, branch_id, team_id, is_active,
		       COALESCE(mfa_enabled, false), created_at, updated_at
		FROM users WHERE email = $1`, email)
	return scanUser(row)
}

func (r *Repository) FindUserByID(ctx context.Context, id uuid.UUID) (*identity.User, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, email, password_hash, full_name, role, branch_id, team_id, is_active,
		       COALESCE(mfa_enabled, false), created_at, updated_at
		FROM users WHERE id = $1`, id)
	return scanUser(row)
}

func (r *Repository) CreateUser(ctx context.Context, user *identity.User) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, full_name, role, branch_id, team_id, is_active, mfa_enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		user.ID, user.Email, user.PasswordHash, user.FullName, user.Role, user.BranchID, user.TeamID,
		user.IsActive, user.MFAEnabled, user.CreatedAt, user.UpdatedAt,
	)
	return err
}

func (r *Repository) UpdateUser(ctx context.Context, user *identity.User) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		UPDATE users SET
			email=$2, password_hash=$3, full_name=$4, role=$5, branch_id=$6, team_id=$7,
			is_active=$8, mfa_enabled=$9, updated_at=$10
		WHERE id=$1`,
		user.ID, user.Email, user.PasswordHash, user.FullName, user.Role, user.BranchID, user.TeamID,
		user.IsActive, user.MFAEnabled, user.UpdatedAt,
	)
	return err
}

func (r *Repository) ListUsers(ctx context.Context, f identity.UserFilter) ([]identity.User, int, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	where := []string{"1=1"}
	args := []any{}
	i := 1
	if f.BranchID != nil {
		where = append(where, fmt.Sprintf("branch_id=$%d", i))
		args = append(args, *f.BranchID)
		i++
	}
	if f.TeamID != nil {
		where = append(where, fmt.Sprintf("team_id=$%d", i))
		args = append(args, *f.TeamID)
		i++
	}
	if f.Role != nil {
		where = append(where, fmt.Sprintf("role=$%d", i))
		args = append(args, string(*f.Role))
		i++
	}
	if f.Active != nil {
		where = append(where, fmt.Sprintf("is_active=$%d", i))
		args = append(args, *f.Active)
		i++
	}
	if strings.TrimSpace(f.Query) != "" {
		where = append(where, fmt.Sprintf("(email ILIKE $%d OR full_name ILIKE $%d)", i, i))
		args = append(args, "%"+strings.TrimSpace(f.Query)+"%")
		i++
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE "+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 25
	}
	args = append(args, limit, f.Offset)
	rows, err := q.Query(ctx, `
		SELECT id, email, password_hash, full_name, role, branch_id, team_id, is_active,
		       COALESCE(mfa_enabled, false), created_at, updated_at
		FROM users WHERE `+w+`
		ORDER BY full_name ASC
		LIMIT $`+fmt.Sprint(i)+` OFFSET $`+fmt.Sprint(i+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []identity.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *u)
	}
	return out, total, rows.Err()
}

func (r *Repository) ListBranches(ctx context.Context) ([]identity.Branch, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	rows, err := q.Query(ctx, `
		SELECT id, code, name_en, name_ar, is_active, created_at FROM branches ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []identity.Branch
	for rows.Next() {
		var b identity.Branch
		if err := rows.Scan(&b.ID, &b.Code, &b.NameEN, &b.NameAR, &b.IsActive, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *Repository) ListTeams(ctx context.Context, branchID *uuid.UUID) ([]identity.Team, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var rows pgx.Rows
	var err error
	if branchID != nil {
		rows, err = q.Query(ctx, `
			SELECT id, branch_id, code, name_en, name_ar, is_active, created_at
			FROM teams WHERE branch_id=$1 ORDER BY code`, *branchID)
	} else {
		rows, err = q.Query(ctx, `
			SELECT id, branch_id, code, name_en, name_ar, is_active, created_at
			FROM teams ORDER BY code`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []identity.Team
	for rows.Next() {
		var t identity.Team
		if err := rows.Scan(&t.ID, &t.BranchID, &t.Code, &t.NameEN, &t.NameAR, &t.IsActive, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) FindTeam(ctx context.Context, id uuid.UUID) (*identity.Team, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	var t identity.Team
	err := q.QueryRow(ctx, `
		SELECT id, branch_id, code, name_en, name_ar, is_active, created_at FROM teams WHERE id=$1`, id).
		Scan(&t.ID, &t.BranchID, &t.Code, &t.NameEN, &t.NameAR, &t.IsActive, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("team: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanUser(row scannable) (*identity.User, error) {
	var u identity.User
	var role string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &role, &u.BranchID, &u.TeamID, &u.IsActive, &u.MFAEnabled, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user: %w", err)
	}
	if err != nil {
		return nil, err
	}
	u.Role = platformauth.Role(role)
	return &u, nil
}
