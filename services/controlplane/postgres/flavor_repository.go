package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
)

type FlavorRepository struct {
	pool *pgxpool.Pool
}

func NewFlavorRepository(pool *pgxpool.Pool) *FlavorRepository {
	return &FlavorRepository{pool: pool}
}

func (r *FlavorRepository) Create(ctx context.Context, f *domain.Flavor) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO flavors (id, name, description, vcpus, memory_mb, disk_gb)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, f.ID, f.Name, f.Description, f.VCpus, f.MemoryMB, f.DiskGB)
	if err != nil {
		return fmt.Errorf("flavor_repo: insert: %w", err)
	}
	return nil
}

func (r *FlavorRepository) GetByID(ctx context.Context, id string) (*domain.Flavor, error) {
	var f domain.Flavor
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, description, vcpus, memory_mb, disk_gb, created_at, updated_at
		FROM flavors WHERE id = $1
	`, id).Scan(&f.ID, &f.Name, &f.Description, &f.VCpus, &f.MemoryMB, &f.DiskGB, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("flavor_repo: get by id: %w", err)
	}
	return &f, nil
}

func (r *FlavorRepository) GetByName(ctx context.Context, name string) (*domain.Flavor, error) {
	var f domain.Flavor
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, description, vcpus, memory_mb, disk_gb, created_at, updated_at
		FROM flavors WHERE name = $1
	`, name).Scan(&f.ID, &f.Name, &f.Description, &f.VCpus, &f.MemoryMB, &f.DiskGB, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("flavor_repo: get by name: %w", err)
	}
	return &f, nil
}

func (r *FlavorRepository) List(ctx context.Context) ([]*domain.Flavor, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, description, vcpus, memory_mb, disk_gb, created_at, updated_at
		FROM flavors ORDER BY vcpus, memory_mb
	`)
	if err != nil {
		return nil, fmt.Errorf("flavor_repo: list: %w", err)
	}
	defer rows.Close()

	var out []*domain.Flavor
	for rows.Next() {
		var f domain.Flavor
		if err := rows.Scan(&f.ID, &f.Name, &f.Description, &f.VCpus, &f.MemoryMB, &f.DiskGB, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("flavor_repo: scan: %w", err)
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (r *FlavorRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM flavors WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("flavor_repo: delete: %w", err)
	}
	return nil
}
