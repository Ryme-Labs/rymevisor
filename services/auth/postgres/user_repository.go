package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/auth/domain"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(ctx context.Context, user *domain.User) error {
	query := `
		INSERT INTO users (email, name, password_hash, status, mfa_enabled, totp_secret)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, email, name, password_hash, status, mfa_enabled, totp_secret, last_login_at, created_at, updated_at`

	return r.pool.QueryRow(ctx, query,
		user.Email, user.Name, user.PasswordHash, user.Status, user.MFAEnabled, user.TOTPSecret,
	).Scan(
		&user.ID, &user.Email, &user.Name, &user.PasswordHash,
		&user.Status, &user.MFAEnabled, &user.TOTPSecret,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	query := `
		SELECT id, email, name, password_hash, status, mfa_enabled, totp_secret, last_login_at, created_at, updated_at
		FROM users WHERE id = $1`

	user := &domain.User{}
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&user.ID, &user.Email, &user.Name, &user.PasswordHash,
		&user.Status, &user.MFAEnabled, &user.TOTPSecret,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	if err := r.populateRelations(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	query := `
		SELECT id, email, name, password_hash, status, mfa_enabled, totp_secret, last_login_at, created_at, updated_at
		FROM users WHERE email = $1`

	user := &domain.User{}
	err := r.pool.QueryRow(ctx, query, email).Scan(
		&user.ID, &user.Email, &user.Name, &user.PasswordHash,
		&user.Status, &user.MFAEnabled, &user.TOTPSecret,
		&user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, domain.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	if err := r.populateRelations(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserRepository) List(ctx context.Context, filter domain.UserFilter) ([]*domain.User, int, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 {
		filter.PerPage = 20
	}
	offset := (filter.Page - 1) * filter.PerPage

	var total int
	countQuery := `
		SELECT COUNT(*) FROM users
		WHERE ($1 = '' OR status::text = $1)
		  AND ($2 = '' OR email ILIKE '%' || $2 || '%' OR name ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR id IN (SELECT user_id FROM user_organizations WHERE organization_id = $3::uuid))`

	err := r.pool.QueryRow(ctx, countQuery, filter.Status, filter.Search, filter.OrganizationID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("count users: %w", err)
	}

	dataQuery := `
		SELECT id, email, name, password_hash, status, mfa_enabled, totp_secret, last_login_at, created_at, updated_at
		FROM users
		WHERE ($1 = '' OR status::text = $1)
		  AND ($2 = '' OR email ILIKE '%' || $2 || '%' OR name ILIKE '%' || $2 || '%')
		  AND ($3 = '' OR id IN (SELECT user_id FROM user_organizations WHERE organization_id = $3::uuid))
		ORDER BY created_at DESC
		LIMIT $4 OFFSET $5`

	rows, err := r.pool.Query(ctx, dataQuery, filter.Status, filter.Search, filter.OrganizationID, filter.PerPage, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []*domain.User
	for rows.Next() {
		u := &domain.User{}
		if err := rows.Scan(
			&u.ID, &u.Email, &u.Name, &u.PasswordHash,
			&u.Status, &u.MFAEnabled, &u.TOTPSecret,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate users: %w", err)
	}

	if len(users) > 0 {
		if err := r.batchPopulateRelations(ctx, users); err != nil {
			return nil, 0, err
		}
	}

	return users, total, nil
}

func (r *UserRepository) batchPopulateRelations(ctx context.Context, users []*domain.User) error {
	ids := make([]string, len(users))
	userMap := make(map[string]*domain.User, len(users))
	for i, u := range users {
		ids[i] = u.ID
		userMap[u.ID] = u
		u.Roles = []string{}
		u.Organizations = []string{}
		u.Permissions = []string{}
	}

	roleRows, err := r.pool.Query(ctx, `SELECT ur.user_id::text, r.name FROM roles r JOIN user_roles ur ON ur.role_id = r.id WHERE ur.user_id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("batch query roles: %w", err)
	}
	defer roleRows.Close()
	for roleRows.Next() {
		var userID, roleName string
		if err := roleRows.Scan(&userID, &roleName); err != nil {
			return fmt.Errorf("scan role: %w", err)
		}
		if u, ok := userMap[userID]; ok {
			u.Roles = append(u.Roles, roleName)
		}
	}
	if err := roleRows.Err(); err != nil {
		return err
	}

	orgRows, err := r.pool.Query(ctx, `SELECT uo.user_id::text, o.slug FROM organizations o JOIN user_organizations uo ON uo.organization_id = o.id WHERE uo.user_id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("batch query orgs: %w", err)
	}
	defer orgRows.Close()
	for orgRows.Next() {
		var userID, slug string
		if err := orgRows.Scan(&userID, &slug); err != nil {
			return fmt.Errorf("scan org: %w", err)
		}
		if u, ok := userMap[userID]; ok {
			u.Organizations = append(u.Organizations, slug)
		}
	}
	if err := orgRows.Err(); err != nil {
		return err
	}

	permRows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ur.user_id::text, p.name FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = ANY($1::uuid[])`, ids)
	if err != nil {
		return fmt.Errorf("batch query permissions: %w", err)
	}
	defer permRows.Close()
	for permRows.Next() {
		var userID, permName string
		if err := permRows.Scan(&userID, &permName); err != nil {
			return fmt.Errorf("scan permission: %w", err)
		}
		if u, ok := userMap[userID]; ok {
			u.Permissions = append(u.Permissions, permName)
		}
	}
	if err := permRows.Err(); err != nil {
		return err
	}

	return nil
}

func (r *UserRepository) Update(ctx context.Context, user *domain.User) error {
	query := `
		UPDATE users SET name = $2, email = $3, status = $4, updated_at = now()
		WHERE id = $1`

	tag, err := r.pool.Exec(ctx, query, user.ID, user.Name, user.Email, user.Status)
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *UserRepository) Delete(ctx context.Context, id string) error {
	query := `UPDATE users SET status = 'deleted', updated_at = now() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *UserRepository) UpdatePassword(ctx context.Context, id string, hash string) error {
	query := `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id, hash)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *UserRepository) UpdateMFA(ctx context.Context, id string, enabled bool, totpSecret string) error {
	query := `UPDATE users SET mfa_enabled = $2, totp_secret = $3, updated_at = now() WHERE id = $1`
	tag, err := r.pool.Exec(ctx, query, id, enabled, totpSecret)
	if err != nil {
		return fmt.Errorf("update mfa: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrUserNotFound
	}
	return nil
}

func (r *UserRepository) populateRelations(ctx context.Context, u *domain.User) error {
	u.Roles = []string{}
	u.Organizations = []string{}
	u.Permissions = []string{}

	roleQuery := `SELECT r.name FROM roles r JOIN user_roles ur ON ur.role_id = r.id WHERE ur.user_id = $1`
	roleRows, err := r.pool.Query(ctx, roleQuery, u.ID)
	if err != nil {
		return fmt.Errorf("query roles: %w", err)
	}
	defer roleRows.Close()
	for roleRows.Next() {
		var name string
		if err := roleRows.Scan(&name); err != nil {
			return fmt.Errorf("scan role: %w", err)
		}
		u.Roles = append(u.Roles, name)
	}
	if err := roleRows.Err(); err != nil {
		return err
	}

	orgQuery := `SELECT o.slug FROM organizations o JOIN user_organizations uo ON uo.organization_id = o.id WHERE uo.user_id = $1`
	orgRows, err := r.pool.Query(ctx, orgQuery, u.ID)
	if err != nil {
		return fmt.Errorf("query orgs: %w", err)
	}
	defer orgRows.Close()
	for orgRows.Next() {
		var slug string
		if err := orgRows.Scan(&slug); err != nil {
			return fmt.Errorf("scan org: %w", err)
		}
		u.Organizations = append(u.Organizations, slug)
	}
	if err := orgRows.Err(); err != nil {
		return err
	}

	permQuery := `
		SELECT DISTINCT p.name FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN user_roles ur ON ur.role_id = rp.role_id
		WHERE ur.user_id = $1`
	permRows, err := r.pool.Query(ctx, permQuery, u.ID)
	if err != nil {
		return fmt.Errorf("query permissions: %w", err)
	}
	defer permRows.Close()
	for permRows.Next() {
		var name string
		if err := permRows.Scan(&name); err != nil {
			return fmt.Errorf("scan permission: %w", err)
		}
		u.Permissions = append(u.Permissions, name)
	}
	if err := permRows.Err(); err != nil {
		return err
	}

	return nil
}

var _ domain.UserRepository = (*UserRepository)(nil)
