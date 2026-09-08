package identity

import (
	"context"
	"errors"
	"fmt"

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
		SELECT id, email, password_hash, full_name, role, branch_id, team_id, is_active, created_at, updated_at
		FROM users WHERE email = $1`, email)
	return scanUser(row)
}

func (r *Repository) FindUserByID(ctx context.Context, id uuid.UUID) (*identity.User, error) {
	q := tx.QuerierFrom(ctx, r.pool)
	row := q.QueryRow(ctx, `
		SELECT id, email, password_hash, full_name, role, branch_id, team_id, is_active, created_at, updated_at
		FROM users WHERE id = $1`, id)
	return scanUser(row)
}

func (r *Repository) CreateUser(ctx context.Context, user *identity.User) error {
	q := tx.QuerierFrom(ctx, r.pool)
	_, err := q.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, full_name, role, branch_id, team_id, is_active, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		user.ID, user.Email, user.PasswordHash, user.FullName, user.Role, user.BranchID, user.TeamID,
		user.IsActive, user.CreatedAt, user.UpdatedAt,
	)
	return err
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

func scanUser(row pgx.Row) (*identity.User, error) {
	var u identity.User
	var role string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &role, &u.BranchID, &u.TeamID, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("user: %w", err)
	}
	if err != nil {
		return nil, err
	}
	u.Role = platformauth.Role(role)
	return &u, nil
}
