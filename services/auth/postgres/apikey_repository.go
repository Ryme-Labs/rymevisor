package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/auth/domain"
)

type APIKeyRepository struct {
	pool *pgxpool.Pool
}

func NewAPIKeyRepository(pool *pgxpool.Pool) *APIKeyRepository {
	return &APIKeyRepository{pool: pool}
}

func (r *APIKeyRepository) Create(ctx context.Context, key *domain.APIKey) error {
	permsJSON, err := json.Marshal(key.Permissions)
	if err != nil {
		return fmt.Errorf("marshal permissions: %w", err)
	}

	var ipStr string
	if len(key.AllowedIPs) > 0 {
		ipStr = strings.Join(key.AllowedIPs, ",")
	}

	query := `
		INSERT INTO api_keys (name, description, prefix, key_hash, user_id, organization_id, permissions, allowed_ips, active, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7,
			CASE WHEN $8 = '' THEN NULL ELSE string_to_array($8, ',')::inet[] END,
			$9, $10)
		RETURNING id, name, description, prefix, key_hash, user_id, organization_id, permissions,
			array_to_string(allowed_ips, ',') as allowed_ips_str, active, expires_at, last_used_at, created_at`

	var allowedIPsStr *string
	return r.pool.QueryRow(ctx, query,
		key.Name, key.Description, key.Prefix, key.KeyHash, key.UserID, key.OrganizationID,
		permsJSON, ipStr, key.Active, key.ExpiresAt,
	).Scan(
		&key.ID, &key.Name, &key.Description, &key.Prefix, &key.KeyHash,
		&key.UserID, &key.OrganizationID, &key.Permissions,
		&allowedIPsStr, &key.Active, &key.ExpiresAt, &key.LastUsedAt, &key.CreatedAt,
	)
}

func (r *APIKeyRepository) GetByID(ctx context.Context, id string) (*domain.APIKey, error) {
	query := `
		SELECT id, name, description, prefix, key_hash, user_id, organization_id, permissions,
			array_to_string(allowed_ips, ',') as allowed_ips_str, active, expires_at, last_used_at, created_at
		FROM api_keys WHERE id = $1`

	return r.scanAPIKey(r.pool.QueryRow(ctx, query, id))
}

func (r *APIKeyRepository) GetByPrefix(ctx context.Context, prefix string) (*domain.APIKey, error) {
	query := `
		SELECT id, name, description, prefix, key_hash, user_id, organization_id, permissions,
			array_to_string(allowed_ips, ',') as allowed_ips_str, active, expires_at, last_used_at, created_at
		FROM api_keys WHERE prefix = $1 AND active = true`

	return r.scanAPIKey(r.pool.QueryRow(ctx, query, prefix))
}

func (r *APIKeyRepository) List(ctx context.Context, organizationID string) ([]*domain.APIKey, error) {
	var query string
	var args []any

	if organizationID != "" {
		query = `
			SELECT id, name, description, prefix, key_hash, user_id, organization_id, permissions,
				array_to_string(allowed_ips, ',') as allowed_ips_str, active, expires_at, last_used_at, created_at
			FROM api_keys WHERE organization_id = $1 ORDER BY created_at DESC`
		args = append(args, organizationID)
	} else {
		query = `
			SELECT id, name, description, prefix, key_hash, user_id, organization_id, permissions,
				array_to_string(allowed_ips, ',') as allowed_ips_str, active, expires_at, last_used_at, created_at
			FROM api_keys ORDER BY created_at DESC`
	}

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []*domain.APIKey
	for rows.Next() {
		key, err := r.scanAPIKeyRow(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (r *APIKeyRepository) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrAPIKeyNotFound
	}
	return nil
}

func (r *APIKeyRepository) UpdateLastUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id)
	return err
}

func (r *APIKeyRepository) scanAPIKey(row pgx.Row) (*domain.APIKey, error) {
	key := &domain.APIKey{}
	var allowedIPsStr *string

	err := row.Scan(
		&key.ID, &key.Name, &key.Description, &key.Prefix, &key.KeyHash,
		&key.UserID, &key.OrganizationID, &key.Permissions,
		&allowedIPsStr, &key.Active, &key.ExpiresAt, &key.LastUsedAt, &key.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, domain.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	if allowedIPsStr != nil && *allowedIPsStr != "" {
		key.AllowedIPs = strings.Split(*allowedIPsStr, ",")
	}
	return key, nil
}

func (r *APIKeyRepository) scanAPIKeyRow(rows pgx.Rows) (*domain.APIKey, error) {
	key := &domain.APIKey{}
	var allowedIPsStr *string

	err := rows.Scan(
		&key.ID, &key.Name, &key.Description, &key.Prefix, &key.KeyHash,
		&key.UserID, &key.OrganizationID, &key.Permissions,
		&allowedIPsStr, &key.Active, &key.ExpiresAt, &key.LastUsedAt, &key.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan api key: %w", err)
	}
	if allowedIPsStr != nil && *allowedIPsStr != "" {
		key.AllowedIPs = strings.Split(*allowedIPsStr, ",")
	}
	return key, nil
}

var _ domain.APIKeyRepository = (*APIKeyRepository)(nil)
