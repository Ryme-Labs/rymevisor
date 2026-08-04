-- Storage service schema

CREATE TYPE storage_driver AS ENUM ('qcow2', 'lvm_thin', 'zfs', 'nfs', 'ceph');
CREATE TYPE volume_status AS ENUM ('available', 'in_use', 'creating', 'deleting', 'error');
CREATE TYPE snapshot_status AS ENUM ('creating', 'ready', 'deleted');

CREATE TABLE storage_pools (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) UNIQUE NOT NULL,
    driver storage_driver NOT NULL DEFAULT 'qcow2',
    path VARCHAR(1024) NOT NULL,
    total_bytes BIGINT NOT NULL DEFAULT 0,
    used_bytes BIGINT NOT NULL DEFAULT 0,
    encrypted BOOLEAN NOT NULL DEFAULT false,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE volumes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    pool_id UUID NOT NULL REFERENCES storage_pools(id) ON DELETE CASCADE,
    size_bytes BIGINT NOT NULL,
    used_bytes BIGINT NOT NULL DEFAULT 0,
    status volume_status NOT NULL DEFAULT 'creating',
    encrypted BOOLEAN NOT NULL DEFAULT false,
    labels JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_volumes_pool ON volumes (pool_id);
CREATE INDEX idx_volumes_status ON volumes (status);

CREATE TABLE volume_snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    volume_id UUID NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    status snapshot_status NOT NULL DEFAULT 'creating',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_volume_snapshots_volume ON volume_snapshots (volume_id);
