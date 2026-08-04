-- Control Plane schema

CREATE TYPE node_status AS ENUM ('online', 'offline', 'draining', 'maintenance', 'error');
CREATE TYPE vm_status AS ENUM ('creating', 'running', 'stopped', 'paused', 'rebooting', 'shutting_down', 'terminated', 'error', 'migrating', 'snapshottting');
CREATE TYPE disk_type AS ENUM ('qcow2', 'raw', 'block');
CREATE TYPE snapshot_status AS ENUM ('creating', 'ready', 'deleted', 'error');

CREATE TABLE nodes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) UNIQUE NOT NULL,
    address VARCHAR(255) NOT NULL,
    port INTEGER NOT NULL DEFAULT 8080,
    status node_status NOT NULL DEFAULT 'online',
    total_cpus INTEGER NOT NULL DEFAULT 0,
    used_cpus INTEGER NOT NULL DEFAULT 0,
    total_memory_mb BIGINT NOT NULL DEFAULT 0,
    used_memory_mb BIGINT NOT NULL DEFAULT 0,
    total_storage_bytes BIGINT NOT NULL DEFAULT 0,
    used_storage_bytes BIGINT NOT NULL DEFAULT 0,
    total_gpus INTEGER NOT NULL DEFAULT 0,
    used_gpus INTEGER NOT NULL DEFAULT 0,
    labels JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    last_heartbeat TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_nodes_status ON nodes (status);
CREATE INDEX idx_nodes_name ON nodes (name);

CREATE TABLE virtual_machines (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    node_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
    organization_id UUID NOT NULL,
    project_id UUID,
    status vm_status NOT NULL DEFAULT 'creating',
    vcpus INTEGER NOT NULL DEFAULT 1,
    memory_mb BIGINT NOT NULL DEFAULT 512,
    cpu_model VARCHAR(255),
    machine_type VARCHAR(50) DEFAULT 'q35',
    enable_secure_boot BOOLEAN NOT NULL DEFAULT false,
    enable_tpm BOOLEAN NOT NULL DEFAULT false,
    hugepages BOOLEAN NOT NULL DEFAULT false,
    cloud_init TEXT,
    ssh_key_id UUID,
    tags JSONB NOT NULL DEFAULT '[]',
    metadata JSONB NOT NULL DEFAULT '{}',
    labels JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_vms_node ON virtual_machines (node_id);
CREATE INDEX idx_vms_organization ON virtual_machines (organization_id);
CREATE INDEX idx_vms_status ON virtual_machines (status);
CREATE INDEX idx_vms_name ON virtual_machines (name);

CREATE TABLE vm_disks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    size_bytes BIGINT NOT NULL,
    type disk_type NOT NULL DEFAULT 'qcow2',
    storage_pool VARCHAR(255),
    boot BOOLEAN NOT NULL DEFAULT false,
    "order" INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_vm_disks_vm ON vm_disks (vm_id);

CREATE TABLE vm_network_interfaces (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    name VARCHAR(50) NOT NULL,
    network_id UUID,
    mac_address VARCHAR(17),
    ipv4_addresses INET[],
    ipv6_addresses INET[],
    is_primary BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_vm_nics_vm ON vm_network_interfaces (vm_id);

CREATE TABLE images (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    os VARCHAR(100) NOT NULL,
    os_version VARCHAR(50),
    architecture VARCHAR(50) NOT NULL DEFAULT 'amd64',
    type VARCHAR(50) NOT NULL DEFAULT 'os',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(50) NOT NULL DEFAULT 'ready',
    checksum VARCHAR(255),
    tags JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_images_os ON images (os);
CREATE INDEX idx_images_arch ON images (architecture);
CREATE INDEX idx_images_type ON images (type);

CREATE TABLE snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    status snapshot_status NOT NULL DEFAULT 'creating',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_snapshots_vm ON snapshots (vm_id);

CREATE TABLE backups (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL DEFAULT 'full',
    status VARCHAR(50) NOT NULL DEFAULT 'creating',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    storage_pool VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_backups_vm ON backups (vm_id);
CREATE INDEX idx_backups_organization ON backups (organization_id);
