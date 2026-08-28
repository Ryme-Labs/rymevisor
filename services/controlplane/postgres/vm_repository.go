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

type VMRepository struct {
	pool *pgxpool.Pool
}

func NewVMRepository(pool *pgxpool.Pool) *VMRepository {
	return &VMRepository{pool: pool}
}

func (r *VMRepository) Create(ctx context.Context, vm *domain.VirtualMachine) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("vm_repo: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tagsJSON, err := json.Marshal(vm.Tags)
	if err != nil {
		return fmt.Errorf("vm_repo: marshal tags: %w", err)
	}
	metadataJSON, err := json.Marshal(vm.Metadata)
	if err != nil {
		return fmt.Errorf("vm_repo: marshal metadata: %w", err)
	}
	labelsJSON, err := json.Marshal(vm.Labels)
	if err != nil {
		return fmt.Errorf("vm_repo: marshal labels: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO virtual_machines (id, name, node_id, organization_id, project_id, status,
			vcpus, memory_mb, cpu_model, machine_type, enable_secure_boot, enable_tpm,
			hugepages, cloud_init, ssh_key_id, tags, metadata, labels)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`,
		vm.ID, vm.Name, vm.NodeID, vm.OrganizationID, vm.ProjectID, vm.Status,
		vm.VCpus, vm.MemoryMB, vm.CPUModel, vm.MachineType, vm.EnableSecureBoot, vm.EnableTPM,
		vm.Hugepages, vm.CloudInit, vm.SSHKeyID, tagsJSON, metadataJSON, labelsJSON,
	)
	if err != nil {
		return fmt.Errorf("vm_repo: insert vm: %w", err)
	}

	for _, disk := range vm.Disks {
		_, err = tx.Exec(ctx, `
			INSERT INTO vm_disks (id, vm_id, name, size_bytes, type, storage_pool, boot, "order")
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`,
			disk.ID, vm.ID, disk.Name, disk.SizeBytes, disk.Type, disk.StoragePool, disk.Boot, disk.Order,
		)
		if err != nil {
			return fmt.Errorf("vm_repo: insert disk: %w", err)
		}
	}

	for _, nic := range vm.NetworkInterfaces {
		_, err = tx.Exec(ctx, `
			INSERT INTO vm_network_interfaces (id, vm_id, name, network_id, mac_address, ipv4_addresses, ipv6_addresses, is_primary)
			VALUES ($1, $2, $3, NULLIF($4, '')::uuid, $5, $6, $7, $8)
		`,
			nic.ID, vm.ID, nic.Name, nic.NetworkID, nic.MACAddress, nic.IPv4Addresses, nic.IPv6Addresses, nic.IsPrimary,
		)
		if err != nil {
			return fmt.Errorf("vm_repo: insert nic: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (r *VMRepository) GetByID(ctx context.Context, id string) (*domain.VirtualMachine, error) {
	vm, err := r.scanVM(ctx, `
		SELECT id, name, node_id, organization_id, project_id, status,
			vcpus, memory_mb, cpu_model, machine_type, enable_secure_boot, enable_tpm,
			hugepages, cloud_init, ssh_key_id, tags, metadata, labels, created_at, updated_at
		FROM virtual_machines WHERE id = $1
	`, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("vm_repo: get by id: %w", err)
	}

	if err := r.loadRelated(ctx, vm); err != nil {
		return nil, err
	}

	return vm, nil
}

func (r *VMRepository) List(ctx context.Context, filter domain.VMFilter) ([]*domain.VirtualMachine, int, error) {
	where := []string{"1=1"}
	args := []any{}
	argIdx := 1

	if filter.OrganizationID != "" {
		where = append(where, fmt.Sprintf("organization_id = $%d", argIdx))
		args = append(args, filter.OrganizationID)
		argIdx++
	}
	if filter.ProjectID != "" {
		where = append(where, fmt.Sprintf("project_id = $%d", argIdx))
		args = append(args, filter.ProjectID)
		argIdx++
	}
	if filter.NodeID != "" {
		where = append(where, fmt.Sprintf("node_id = $%d", argIdx))
		args = append(args, filter.NodeID)
		argIdx++
	}
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
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM virtual_machines WHERE %s", whereClause)
	err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("vm_repo: count: %w", err)
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
		SELECT id, name, node_id, organization_id, project_id, status,
			vcpus, memory_mb, cpu_model, machine_type, enable_secure_boot, enable_tpm,
			hugepages, cloud_init, ssh_key_id, tags, metadata, labels, created_at, updated_at
		FROM virtual_machines
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)
	args = append(args, perPage, offset)

	rows, err := r.pool.Query(ctx, listQuery, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("vm_repo: list query: %w", err)
	}
	defer rows.Close()

	var vms []*domain.VirtualMachine
	for rows.Next() {
		vm, err := r.scanVMFromRows(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("vm_repo: scan vm: %w", err)
		}
		vms = append(vms, vm)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("vm_repo: rows error: %w", err)
	}

	for _, vm := range vms {
		if err := r.loadRelated(ctx, vm); err != nil {
			return nil, 0, err
		}
	}

	return vms, total, nil
}

func (r *VMRepository) Update(ctx context.Context, vm *domain.VirtualMachine) error {
	tagsJSON, err := json.Marshal(vm.Tags)
	if err != nil {
		return fmt.Errorf("vm_repo: marshal tags: %w", err)
	}
	metadataJSON, err := json.Marshal(vm.Metadata)
	if err != nil {
		return fmt.Errorf("vm_repo: marshal metadata: %w", err)
	}
	labelsJSON, err := json.Marshal(vm.Labels)
	if err != nil {
		return fmt.Errorf("vm_repo: marshal labels: %w", err)
	}

	_, err = r.pool.Exec(ctx, `
		UPDATE virtual_machines
		SET name = $1, metadata = $2, labels = $3, tags = $4, updated_at = now()
		WHERE id = $5
	`, vm.Name, metadataJSON, labelsJSON, tagsJSON, vm.ID)
	if err != nil {
		return fmt.Errorf("vm_repo: update: %w", err)
	}
	return nil
}

func (r *VMRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "DELETE FROM virtual_machines WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("vm_repo: delete: %w", err)
	}
	return nil
}

func (r *VMRepository) UpdateStatus(ctx context.Context, id string, status domain.VMStatus) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE virtual_machines SET status = $1, updated_at = now() WHERE id = $2
	`, status, id)
	if err != nil {
		return fmt.Errorf("vm_repo: update status: %w", err)
	}
	return nil
}

func (r *VMRepository) loadRelated(ctx context.Context, vm *domain.VirtualMachine) error {
	diskRows, err := r.pool.Query(ctx, `
		SELECT id, name, size_bytes, type, storage_pool, boot, "order"
		FROM vm_disks WHERE vm_id = $1 ORDER BY "order"
	`, vm.ID)
	if err != nil {
		return fmt.Errorf("vm_repo: query disks: %w", err)
	}
	defer diskRows.Close()

	for diskRows.Next() {
		var d domain.Disk
		if err := diskRows.Scan(&d.ID, &d.Name, &d.SizeBytes, &d.Type, &d.StoragePool, &d.Boot, &d.Order); err != nil {
			return fmt.Errorf("vm_repo: scan disk: %w", err)
		}
		vm.Disks = append(vm.Disks, d)
	}
	if err := diskRows.Err(); err != nil {
		return err
	}

	nicRows, err := r.pool.Query(ctx, `
		SELECT id, name, network_id, mac_address, ipv4_addresses, ipv6_addresses, is_primary
		FROM vm_network_interfaces WHERE vm_id = $1
	`, vm.ID)
	if err != nil {
		return fmt.Errorf("vm_repo: query nics: %w", err)
	}
	defer nicRows.Close()

	for nicRows.Next() {
		var n domain.NetworkInterface
		var networkID *string
		if err := nicRows.Scan(&n.ID, &n.Name, &networkID, &n.MACAddress, &n.IPv4Addresses, &n.IPv6Addresses, &n.IsPrimary); err != nil {
			return fmt.Errorf("vm_repo: scan nic: %w", err)
		}
		if networkID != nil {
			n.NetworkID = *networkID
		}
		vm.NetworkInterfaces = append(vm.NetworkInterfaces, n)
	}
	if err := nicRows.Err(); err != nil {
		return err
	}

	return nil
}

func (r *VMRepository) scanVM(ctx context.Context, query string, args ...any) (*domain.VirtualMachine, error) {
	var vm domain.VirtualMachine
	var tagsJSON, metadataJSON, labelsJSON []byte
	var nodeID, projectID, sshKeyID *string

	err := r.pool.QueryRow(ctx, query, args...).Scan(
		&vm.ID, &vm.Name, &nodeID, &vm.OrganizationID, &projectID, &vm.Status,
		&vm.VCpus, &vm.MemoryMB, &vm.CPUModel, &vm.MachineType, &vm.EnableSecureBoot, &vm.EnableTPM,
		&vm.Hugepages, &vm.CloudInit, &sshKeyID, &tagsJSON, &metadataJSON, &labelsJSON,
		&vm.CreatedAt, &vm.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	vm.NodeID = nodeID
	vm.ProjectID = projectID
	vm.SSHKeyID = sshKeyID

	if err := json.Unmarshal(tagsJSON, &vm.Tags); err != nil {
		return nil, fmt.Errorf("vm_repo: unmarshal tags: %w", err)
	}
	if err := json.Unmarshal(metadataJSON, &vm.Metadata); err != nil {
		return nil, fmt.Errorf("vm_repo: unmarshal metadata: %w", err)
	}
	if err := json.Unmarshal(labelsJSON, &vm.Labels); err != nil {
		return nil, fmt.Errorf("vm_repo: unmarshal labels: %w", err)
	}

	return &vm, nil
}

func (r *VMRepository) scanVMFromRows(rows pgx.Rows) (*domain.VirtualMachine, error) {
	var vm domain.VirtualMachine
	var tagsJSON, metadataJSON, labelsJSON []byte
	var nodeID, projectID, sshKeyID *string

	err := rows.Scan(
		&vm.ID, &vm.Name, &nodeID, &vm.OrganizationID, &projectID, &vm.Status,
		&vm.VCpus, &vm.MemoryMB, &vm.CPUModel, &vm.MachineType, &vm.EnableSecureBoot, &vm.EnableTPM,
		&vm.Hugepages, &vm.CloudInit, &sshKeyID, &tagsJSON, &metadataJSON, &labelsJSON,
		&vm.CreatedAt, &vm.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	vm.NodeID = nodeID
	vm.ProjectID = projectID
	vm.SSHKeyID = sshKeyID

	if err := json.Unmarshal(tagsJSON, &vm.Tags); err != nil {
		return nil, fmt.Errorf("vm_repo: unmarshal tags: %w", err)
	}
	if err := json.Unmarshal(metadataJSON, &vm.Metadata); err != nil {
		return nil, fmt.Errorf("vm_repo: unmarshal metadata: %w", err)
	}
	if err := json.Unmarshal(labelsJSON, &vm.Labels); err != nil {
		return nil, fmt.Errorf("vm_repo: unmarshal labels: %w", err)
	}

	return &vm, nil
}
