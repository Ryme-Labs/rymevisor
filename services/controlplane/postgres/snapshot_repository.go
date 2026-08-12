package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
)

type SnapshotRepository struct {
	pool *pgxpool.Pool
}

func NewSnapshotRepository(pool *pgxpool.Pool) *SnapshotRepository {
	return &SnapshotRepository{pool: pool}
}

func (r *SnapshotRepository) Create(ctx context.Context, snap *domain.Snapshot) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO snapshots (id, vm_id, name, description, size_bytes, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`,
		snap.ID, snap.VMID, snap.Name, snap.Description, snap.SizeBytes, snap.Status,
	)
	if err != nil {
		return fmt.Errorf("snapshot_repo: insert: %w", err)
	}
	return nil
}

func (r *SnapshotRepository) GetByID(ctx context.Context, id string) (*domain.Snapshot, error) {
	var s domain.Snapshot
	err := r.pool.QueryRow(ctx, `
		SELECT id, vm_id, name, description, size_bytes, status, created_at
		FROM snapshots WHERE id = $1
	`, id).Scan(&s.ID, &s.VMID, &s.Name, &s.Description, &s.SizeBytes, &s.Status, &s.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("snapshot_repo: get by id: %w", err)
	}
	return &s, nil
}

func (r *SnapshotRepository) ListByVM(ctx context.Context, vmID string) ([]*domain.Snapshot, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, vm_id, name, description, size_bytes, status, created_at
		FROM snapshots WHERE vm_id = $1 ORDER BY created_at DESC
	`, vmID)
	if err != nil {
		return nil, fmt.Errorf("snapshot_repo: list by vm: %w", err)
	}
	defer rows.Close()

	var snapshots []*domain.Snapshot
	for rows.Next() {
		var s domain.Snapshot
		if err := rows.Scan(&s.ID, &s.VMID, &s.Name, &s.Description, &s.SizeBytes, &s.Status, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("snapshot_repo: scan: %w", err)
		}
		snapshots = append(snapshots, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("snapshot_repo: rows error: %w", err)
	}
	return snapshots, nil
}

func (r *SnapshotRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM snapshots WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("snapshot_repo: delete: %w", err)
	}
	return nil
}
