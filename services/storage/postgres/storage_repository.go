package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/storage/domain"
)









type StoragePoolRepository struct {
	pool *pgxpool.Pool
}

func NewStoragePoolRepository(pool *pgxpool.Pool) *StoragePoolRepository {
	return &StoragePoolRepository{pool: pool}
}

func (r *StoragePoolRepository) Create(ctx context.Context, p *domain.StoragePool) error {
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO storage_pools (id, name, driver, path, total_bytes, used_bytes, encrypted, config)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		p.ID, p.Name, p.Driver, p.Path, p.TotalBytes, p.UsedBytes, p.Encrypted, configJSON,
	)
	return err
}

func (r *StoragePoolRepository) GetByID(ctx context.Context, id string) (*domain.StoragePool, error) {
	var p domain.StoragePool
	var configJSON []byte

	err := r.pool.QueryRow(ctx,
		`SELECT id, name, driver, path, total_bytes, used_bytes, encrypted, config, created_at, updated_at
		 FROM storage_pools WHERE id = $1`, id,
	).Scan(&p.ID, &p.Name, &p.Driver, &p.Path, &p.TotalBytes, &p.UsedBytes, &p.Encrypted, &configJSON, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if configJSON != nil {
		_ = json.Unmarshal(configJSON, &p.Config)
	}

	return &p, nil
}

func (r *StoragePoolRepository) List(ctx context.Context) ([]*domain.StoragePool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, driver, path, total_bytes, used_bytes, encrypted, config, created_at, updated_at
		 FROM storage_pools ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pools []*domain.StoragePool
	for rows.Next() {
		p := &domain.StoragePool{}
		var configJSON []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Driver, &p.Path, &p.TotalBytes, &p.UsedBytes, &p.Encrypted, &configJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if configJSON != nil {
			_ = json.Unmarshal(configJSON, &p.Config)
		}
		pools = append(pools, p)
	}
	return pools, rows.Err()
}

func (r *StoragePoolRepository) Update(ctx context.Context, p *domain.StoragePool) error {
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`UPDATE storage_pools SET name=$2, driver=$3, path=$4, total_bytes=$5, used_bytes=$6, encrypted=$7, config=$8, updated_at=now() WHERE id=$1`,
		p.ID, p.Name, p.Driver, p.Path, p.TotalBytes, p.UsedBytes, p.Encrypted, configJSON,
	)
	return err
}

type VolumeRepository struct {
	pool *pgxpool.Pool
}

func NewVolumeRepository(pool *pgxpool.Pool) *VolumeRepository {
	return &VolumeRepository{pool: pool}
}

func (r *VolumeRepository) Create(ctx context.Context, vol *domain.Volume) error {
	labelsJSON, err := json.Marshal(vol.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO volumes (id, name, pool_id, size_bytes, used_bytes, status, encrypted, labels)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		vol.ID, vol.Name, vol.PoolID, vol.SizeBytes, vol.UsedBytes, vol.Status, vol.Encrypted, labelsJSON,
	)
	return err
}

func (r *VolumeRepository) GetByID(ctx context.Context, id string) (*domain.Volume, error) {
	var vol domain.Volume
	var labelsJSON []byte

	err := r.pool.QueryRow(ctx,
		`SELECT id, name, pool_id, size_bytes, used_bytes, status, encrypted, labels, created_at, updated_at
		 FROM volumes WHERE id = $1`, id,
	).Scan(&vol.ID, &vol.Name, &vol.PoolID, &vol.SizeBytes, &vol.UsedBytes, &vol.Status, &vol.Encrypted, &labelsJSON, &vol.CreatedAt, &vol.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if labelsJSON != nil {
		_ = json.Unmarshal(labelsJSON, &vol.Labels)
	}

	snapshots, err := r.listSnapshots(ctx, id)
	if err != nil {
		return nil, err
	}
	vol.Snapshots = snapshots

	return &vol, nil
}

func (r *VolumeRepository) listSnapshots(ctx context.Context, volumeID string) ([]domain.VolumeSnapshot, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, volume_id, name, size_bytes, status, created_at
		 FROM volume_snapshots WHERE volume_id = $1`, volumeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []domain.VolumeSnapshot
	for rows.Next() {
		var snap domain.VolumeSnapshot
		if err := rows.Scan(&snap.ID, &snap.VolumeID, &snap.Name, &snap.SizeBytes, &snap.Status, &snap.CreatedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

func (r *VolumeRepository) List(ctx context.Context, filter domain.VolumeFilter) ([]*domain.Volume, int, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PerPage < 1 {
		filter.PerPage = 20
	}
	where := ""
	args := []any{}
	argIdx := 1
	if filter.PoolID != "" {
		where = fmt.Sprintf(" WHERE pool_id = $%d::uuid", argIdx)
		args = append(args, filter.PoolID)
		argIdx++
	}
	var total int
	countQuery := "SELECT COUNT(*) FROM volumes" + where
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.PerPage
	dataArgs := append([]any{}, args...)
	dataArgs = append(dataArgs, filter.PerPage, offset)
	listQuery := fmt.Sprintf(`SELECT id, name, pool_id, size_bytes, used_bytes, status, encrypted, labels, created_at, updated_at
		 FROM volumes%s
		 ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, argIdx, argIdx+1)
	rows, err := r.pool.Query(ctx, listQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var volumes []*domain.Volume
	for rows.Next() {
		vol := &domain.Volume{}
		var labelsJSON []byte
		if err := rows.Scan(&vol.ID, &vol.Name, &vol.PoolID, &vol.SizeBytes, &vol.UsedBytes, &vol.Status, &vol.Encrypted, &labelsJSON, &vol.CreatedAt, &vol.UpdatedAt); err != nil {
			return nil, 0, err
		}
		if labelsJSON != nil {
			_ = json.Unmarshal(labelsJSON, &vol.Labels)
		}
		volumes = append(volumes, vol)
	}
	return volumes, total, rows.Err()
}

func (r *VolumeRepository) Update(ctx context.Context, vol *domain.Volume) error {
	labelsJSON, err := json.Marshal(vol.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`UPDATE volumes SET name = $2, size_bytes = $3, used_bytes = $4, status = $5, encrypted = $6, labels = $7, updated_at = now()
		 WHERE id = $1`,
		vol.ID, vol.Name, vol.SizeBytes, vol.UsedBytes, vol.Status, vol.Encrypted, labelsJSON,
	)
	return err
}

func (r *VolumeRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM volumes WHERE id = $1`, id)
	return err
}
