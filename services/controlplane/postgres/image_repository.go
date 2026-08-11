package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
)

type ImageRepository struct {
	pool *pgxpool.Pool
}

func NewImageRepository(pool *pgxpool.Pool) *ImageRepository {
	return &ImageRepository{pool: pool}
}

func (r *ImageRepository) Create(ctx context.Context, img *domain.Image) error {
	tagsJSON, err := json.Marshal(img.Tags)
	if err != nil {
		return fmt.Errorf("image_repo: marshal tags: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO images (id, name, description, os, os_version, architecture, type,
			size_bytes, status, checksum, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		img.ID, img.Name, img.Description, img.OS, img.OSVersion, img.Architecture,
		img.Type, img.SizeBytes, img.Status, img.Checksum, tagsJSON,
	)
	if err != nil {
		return fmt.Errorf("image_repo: insert: %w", err)
	}
	return nil
}

func (r *ImageRepository) GetByID(ctx context.Context, id string) (*domain.Image, error) {
	var img domain.Image
	var tagsJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, name, description, os, os_version, architecture, type,
			size_bytes, status, checksum, tags, created_at, updated_at
		FROM images WHERE id = $1
	`, id).Scan(
		&img.ID, &img.Name, &img.Description, &img.OS, &img.OSVersion, &img.Architecture,
		&img.Type, &img.SizeBytes, &img.Status, &img.Checksum, &tagsJSON,
		&img.CreatedAt, &img.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("image_repo: get by id: %w", err)
	}

	if err := json.Unmarshal(tagsJSON, &img.Tags); err != nil {
		return nil, fmt.Errorf("image_repo: unmarshal tags: %w", err)
	}

	return &img, nil
}

func (r *ImageRepository) List(ctx context.Context, filter domain.ImageFilter) ([]*domain.Image, int, error) {
	where := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if filter.OS != "" {
		where = append(where, fmt.Sprintf("os = $%d", argIdx))
		args = append(args, filter.OS)
		argIdx++
	}
	if filter.Architecture != "" {
		where = append(where, fmt.Sprintf("architecture = $%d", argIdx))
		args = append(args, filter.Architecture)
		argIdx++
	}
	if filter.Type != "" {
		where = append(where, fmt.Sprintf("type = $%d", argIdx))
		args = append(args, filter.Type)
		argIdx++
	}
	if filter.Search != "" {
		where = append(where, fmt.Sprintf("name ILIKE $%d", argIdx))
		args = append(args, "%"+filter.Search+"%")
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM images WHERE %s", whereClause)
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("image_repo: count: %w", err)
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
		SELECT id, name, description, os, os_version, architecture, type,
			size_bytes, status, checksum, tags, created_at, updated_at
		FROM images
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, perPage, offset)

	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("image_repo: list query: %w", err)
	}
	defer rows.Close()

	var images []*domain.Image
	for rows.Next() {
		var img domain.Image
		var tagsJSON []byte

		err := rows.Scan(
			&img.ID, &img.Name, &img.Description, &img.OS, &img.OSVersion, &img.Architecture,
			&img.Type, &img.SizeBytes, &img.Status, &img.Checksum, &tagsJSON,
			&img.CreatedAt, &img.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("image_repo: scan: %w", err)
		}
		if err := json.Unmarshal(tagsJSON, &img.Tags); err != nil {
			return nil, 0, fmt.Errorf("image_repo: unmarshal tags: %w", err)
		}
		images = append(images, &img)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("image_repo: rows error: %w", err)
	}

	return images, total, nil
}

func (r *ImageRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM images WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("image_repo: delete: %w", err)
	}
	return nil
}
