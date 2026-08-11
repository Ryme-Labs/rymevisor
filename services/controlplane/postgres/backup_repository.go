package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
)

type BackupRepository struct {
	pool *pgxpool.Pool
}

func NewBackupRepository(pool *pgxpool.Pool) *BackupRepository {
	return &BackupRepository{pool: pool}
}

func (r *BackupRepository) Create(ctx context.Context, backup *domain.Backup) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO backups (id, vm_id, organization_id, name, type, status, size_bytes, storage_pool)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		backup.ID, backup.VMID, backup.OrganizationID, backup.Name,
		backup.Type, backup.Status, backup.SizeBytes, backup.StoragePool,
	)
	if err != nil {
		return fmt.Errorf("backup_repo: insert: %w", err)
	}
	return nil
}

func (r *BackupRepository) GetByID(ctx context.Context, id string) (*domain.Backup, error) {
	var b domain.Backup
	err := r.pool.QueryRow(ctx, `
		SELECT id, vm_id, organization_id, name, type, status, size_bytes, storage_pool,
			created_at, completed_at
		FROM backups WHERE id = $1
	`, id).Scan(
		&b.ID, &b.VMID, &b.OrganizationID, &b.Name, &b.Type, &b.Status,
		&b.SizeBytes, &b.StoragePool, &b.CreatedAt, &b.CompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("backup_repo: get by id: %w", err)
	}
	return &b, nil
}

func (r *BackupRepository) List(ctx context.Context, filter domain.BackupFilter) ([]*domain.Backup, int, error) {
	where := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if filter.VMID != "" {
		where = append(where, fmt.Sprintf("vm_id = $%d", argIdx))
		args = append(args, filter.VMID)
		argIdx++
	}
	if filter.OrganizationID != "" {
		where = append(where, fmt.Sprintf("organization_id = $%d", argIdx))
		args = append(args, filter.OrganizationID)
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM backups WHERE %s", whereClause)
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("backup_repo: count: %w", err)
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = 20
	}
	offset := (page - 1) * perPage

	listQuery := fmt.Sprintf(`
		SELECT id, vm_id, organization_id, name, type, status, size_bytes, storage_pool,
			created_at, completed_at
		FROM backups
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, perPage, offset)

	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("backup_repo: list query: %w", err)
	}
	defer rows.Close()

	var backups []*domain.Backup
	for rows.Next() {
		var b domain.Backup
		err := rows.Scan(
			&b.ID, &b.VMID, &b.OrganizationID, &b.Name, &b.Type, &b.Status,
			&b.SizeBytes, &b.StoragePool, &b.CreatedAt, &b.CompletedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("backup_repo: scan: %w", err)
		}
		backups = append(backups, &b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("backup_repo: rows error: %w", err)
	}

	return backups, total, nil
}

func (r *BackupRepository) Update(ctx context.Context, backup *domain.Backup) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE backups
		SET status = $1, size_bytes = $2, completed_at = $3
		WHERE id = $4
	`, backup.Status, backup.SizeBytes, backup.CompletedAt, backup.ID)
	if err != nil {
		return fmt.Errorf("backup_repo: update: %w", err)
	}
	return nil
}

func (r *BackupRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM backups WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("backup_repo: delete: %w", err)
	}
	return nil
}
