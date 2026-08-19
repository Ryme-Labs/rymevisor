package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/storage/domain"
)

type SnapshotRepository struct {
	pool *pgxpool.Pool
}

func NewSnapshotRepository(pool *pgxpool.Pool) *SnapshotRepository {
	return &SnapshotRepository{pool: pool}
}

func (r *SnapshotRepository) Create(ctx context.Context, snap *domain.VolumeSnapshot) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO volume_snapshots (id, volume_id, name, size_bytes, status)
		 VALUES ($1, $2, $3, $4, $5)`,
		snap.ID, snap.VolumeID, snap.Name, snap.SizeBytes, snap.Status,
	)
	return err
}

func (r *SnapshotRepository) GetByID(ctx context.Context, id string) (*domain.VolumeSnapshot, error) {
	var snap domain.VolumeSnapshot
	err := r.pool.QueryRow(ctx,
		`SELECT id, volume_id, name, size_bytes, status, created_at
		 FROM volume_snapshots WHERE id = $1`, id,
	).Scan(&snap.ID, &snap.VolumeID, &snap.Name, &snap.SizeBytes, &snap.Status, &snap.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &snap, nil
}

func (r *SnapshotRepository) ListByVolume(ctx context.Context, volumeID string) ([]*domain.VolumeSnapshot, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, volume_id, name, size_bytes, status, created_at
		 FROM volume_snapshots WHERE volume_id = $1`, volumeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snapshots []*domain.VolumeSnapshot
	for rows.Next() {
		snap := &domain.VolumeSnapshot{}
		if err := rows.Scan(&snap.ID, &snap.VolumeID, &snap.Name, &snap.SizeBytes, &snap.Status, &snap.CreatedAt); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

func (r *SnapshotRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM volume_snapshots WHERE id = $1`, id)
	return err
}
