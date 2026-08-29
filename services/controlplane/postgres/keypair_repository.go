package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
)

type KeypairRepository struct {
	pool *pgxpool.Pool
}

func NewKeypairRepository(pool *pgxpool.Pool) *KeypairRepository {
	return &KeypairRepository{pool: pool}
}

func (r *KeypairRepository) Create(ctx context.Context, k *domain.Keypair) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO keypairs (id, name, public_key, fingerprint, organization_id)
		VALUES ($1, $2, $3, $4, $5)
	`, k.ID, k.Name, k.PublicKey, k.Fingerprint, k.OrganizationID)
	if err != nil {
		return fmt.Errorf("keypair_repo: insert: %w", err)
	}
	return nil
}

func (r *KeypairRepository) GetByID(ctx context.Context, id string) (*domain.Keypair, error) {
	var k domain.Keypair
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, public_key, fingerprint, organization_id, created_at
		FROM keypairs WHERE id = $1
	`, id).Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.OrganizationID, &k.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("keypair_repo: get by id: %w", err)
	}
	return &k, nil
}

func (r *KeypairRepository) GetByName(ctx context.Context, name string, orgID string) (*domain.Keypair, error) {
	var k domain.Keypair
	query := `SELECT id, name, public_key, fingerprint, organization_id, created_at FROM keypairs WHERE name = $1`
	args := []any{name}
	if orgID != "" {
		query += ` AND organization_id = $2`
		args = append(args, orgID)
	}
	query += ` LIMIT 1`
	err := r.pool.QueryRow(ctx, query, args...).Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.OrganizationID, &k.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("keypair_repo: get by name: %w", err)
	}
	return &k, nil
}

func (r *KeypairRepository) List(ctx context.Context, orgID string) ([]*domain.Keypair, error) {
	query := `SELECT id, name, public_key, fingerprint, organization_id, created_at FROM keypairs`
	var args []any
	if orgID != "" {
		query += ` WHERE organization_id = $1`
		args = append(args, orgID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("keypair_repo: list: %w", err)
	}
	defer rows.Close()

	var out []*domain.Keypair
	for rows.Next() {
		var k domain.Keypair
		if err := rows.Scan(&k.ID, &k.Name, &k.PublicKey, &k.Fingerprint, &k.OrganizationID, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("keypair_repo: scan: %w", err)
		}
		out = append(out, &k)
	}
	return out, rows.Err()
}

func (r *KeypairRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM keypairs WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("keypair_repo: delete: %w", err)
	}
	return nil
}
