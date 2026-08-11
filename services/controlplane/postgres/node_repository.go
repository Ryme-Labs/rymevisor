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

type NodeRepository struct {
	pool *pgxpool.Pool
}

func NewNodeRepository(pool *pgxpool.Pool) *NodeRepository {
	return &NodeRepository{pool: pool}
}

func (r *NodeRepository) Create(ctx context.Context, node *domain.Node) error {
	labelsJSON, err := json.Marshal(node.Labels)
	if err != nil {
		return fmt.Errorf("node_repo: marshal labels: %w", err)
	}
	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("node_repo: marshal metadata: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		INSERT INTO nodes (id, name, address, port, status,
			total_cpus, used_cpus, total_memory_mb, used_memory_mb,
			total_storage_bytes, used_storage_bytes, total_gpus, used_gpus,
			labels, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`,
		node.ID, node.Name, node.Address, node.Port, node.Status,
		node.TotalCPUs, node.UsedCPUs, node.TotalMemoryMB, node.UsedMemoryMB,
		node.TotalStorageBytes, node.UsedStorageBytes, node.TotalGPUs, node.UsedGPUs,
		labelsJSON, metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("node_repo: insert: %w", err)
	}
	return nil
}

func (r *NodeRepository) GetByID(ctx context.Context, id string) (*domain.Node, error) {
	node, err := r.scanNode(ctx, `
		SELECT id, name, address, port, status,
			total_cpus, used_cpus, total_memory_mb, used_memory_mb,
			total_storage_bytes, used_storage_bytes, total_gpus, used_gpus,
			labels, metadata, last_heartbeat, created_at, updated_at
		FROM nodes WHERE id = $1
	`, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("node_repo: get by id: %w", err)
	}
	return node, nil
}

func (r *NodeRepository) GetByName(ctx context.Context, name string) (*domain.Node, error) {
	node, err := r.scanNode(ctx, `
		SELECT id, name, address, port, status,
			total_cpus, used_cpus, total_memory_mb, used_memory_mb,
			total_storage_bytes, used_storage_bytes, total_gpus, used_gpus,
			labels, metadata, last_heartbeat, created_at, updated_at
		FROM nodes WHERE name = $1
	`, name)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("node_repo: get by name: %w", err)
	}
	return node, nil
}

func (r *NodeRepository) List(ctx context.Context, filter domain.NodeFilter) ([]*domain.Node, int, error) {
	where := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if filter.Status != "" {
		where = append(where, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, filter.Status)
		argIdx++
	}
	if filter.Search != "" {
		where = append(where, fmt.Sprintf("name ILIKE $%d", argIdx))
		args = append(args, "%"+filter.Search+"%")
		argIdx++
	}

	whereClause := strings.Join(where, " AND ")

	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM nodes WHERE %s", whereClause)
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("node_repo: count: %w", err)
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
		SELECT id, name, address, port, status,
			total_cpus, used_cpus, total_memory_mb, used_memory_mb,
			total_storage_bytes, used_storage_bytes, total_gpus, used_gpus,
			labels, metadata, last_heartbeat, created_at, updated_at
		FROM nodes
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, perPage, offset)

	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("node_repo: list query: %w", err)
	}
	defer rows.Close()

	var nodes []*domain.Node
	for rows.Next() {
		node, err := r.scanNodeFromRows(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("node_repo: scan node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("node_repo: rows error: %w", err)
	}

	return nodes, total, nil
}

func (r *NodeRepository) Update(ctx context.Context, node *domain.Node) error {
	labelsJSON, err := json.Marshal(node.Labels)
	if err != nil {
		return fmt.Errorf("node_repo: marshal labels: %w", err)
	}
	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return fmt.Errorf("node_repo: marshal metadata: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE nodes
		SET labels = $1, metadata = $2, updated_at = now()
		WHERE id = $3
	`, labelsJSON, metadataJSON, node.ID)
	if err != nil {
		return fmt.Errorf("node_repo: update: %w", err)
	}
	return nil
}

func (r *NodeRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM nodes WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("node_repo: delete: %w", err)
	}
	return nil
}

func (r *NodeRepository) UpdateStatus(ctx context.Context, id string, status domain.NodeStatus) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE nodes SET status = $1, updated_at = now() WHERE id = $2
	`, status, id)
	if err != nil {
		return fmt.Errorf("node_repo: update status: %w", err)
	}
	return nil
}

func (r *NodeRepository) UpdateHeartbeat(ctx context.Context, id string, resources domain.NodeResources) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE nodes
		SET total_cpus = $1, used_cpus = $2, total_memory_mb = $3, used_memory_mb = $4,
			total_storage_bytes = $5, used_storage_bytes = $6, total_gpus = $7, used_gpus = $8,
			last_heartbeat = now(), updated_at = now()
		WHERE id = $9
	`,
		resources.TotalCPUs, resources.UsedCPUs,
		resources.TotalMemoryMB, resources.UsedMemoryMB,
		resources.TotalStorageBytes, resources.UsedStorageBytes,
		resources.TotalGPUs, resources.UsedGPUs,
		id,
	)
	if err != nil {
		return fmt.Errorf("node_repo: update heartbeat: %w", err)
	}
	return nil
}

func (r *NodeRepository) scanNode(ctx context.Context, query string, args ...any) (*domain.Node, error) {
	var node domain.Node
	var labelsJSON, metadataJSON []byte

	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&node.ID, &node.Name, &node.Address, &node.Port, &node.Status,
		&node.TotalCPUs, &node.UsedCPUs, &node.TotalMemoryMB, &node.UsedMemoryMB,
		&node.TotalStorageBytes, &node.UsedStorageBytes, &node.TotalGPUs, &node.UsedGPUs,
		&labelsJSON, &metadataJSON, &node.LastHeartbeat, &node.CreatedAt, &node.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(labelsJSON, &node.Labels); err != nil {
		return nil, fmt.Errorf("node_repo: unmarshal labels: %w", err)
	}
	if err := json.Unmarshal(metadataJSON, &node.Metadata); err != nil {
		return nil, fmt.Errorf("node_repo: unmarshal metadata: %w", err)
	}

	return &node, nil
}

func (r *NodeRepository) scanNodeFromRows(rows pgx.Rows) (*domain.Node, error) {
	var node domain.Node
	var labelsJSON, metadataJSON []byte

	err := rows.Scan(
		&node.ID, &node.Name, &node.Address, &node.Port, &node.Status,
		&node.TotalCPUs, &node.UsedCPUs, &node.TotalMemoryMB, &node.UsedMemoryMB,
		&node.TotalStorageBytes, &node.UsedStorageBytes, &node.TotalGPUs, &node.UsedGPUs,
		&labelsJSON, &metadataJSON, &node.LastHeartbeat, &node.CreatedAt, &node.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(labelsJSON, &node.Labels); err != nil {
		return nil, fmt.Errorf("node_repo: unmarshal labels: %w", err)
	}
	if err := json.Unmarshal(metadataJSON, &node.Metadata); err != nil {
		return nil, fmt.Errorf("node_repo: unmarshal metadata: %w", err)
	}

	return &node, nil
}
