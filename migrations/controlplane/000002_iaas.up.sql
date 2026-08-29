-- IaaS production-grade: images source_url, vm_disks image_id, flavors, keypairs

-- Images: track source URL for auto-pulled cloud images
ALTER TABLE images ADD COLUMN IF NOT EXISTS source_url TEXT;

-- VM disks: reference to image for backing file (like AWS AMI)
ALTER TABLE vm_disks ADD COLUMN IF NOT EXISTS image_id UUID REFERENCES images(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_vm_disks_image ON vm_disks (image_id);

-- Flavors: instance types for IaaS (like AWS EC2 types)
CREATE TABLE IF NOT EXISTS flavors (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) UNIQUE NOT NULL,
    description TEXT,
    vcpus INTEGER NOT NULL,
    memory_mb BIGINT NOT NULL,
    disk_gb BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed default flavors
INSERT INTO flavors (name, description, vcpus, memory_mb, disk_gb) VALUES
    ('small', '1 vCPU, 1GB RAM, 20GB disk', 1, 1024, 20),
    ('medium', '2 vCPU, 4GB RAM, 40GB disk', 2, 4096, 40),
    ('large', '4 vCPU, 8GB RAM, 80GB disk', 4, 8192, 80),
    ('xlarge', '8 vCPU, 16GB RAM, 160GB disk', 8, 16384, 160),
    ('2xlarge', '16 vCPU, 32GB RAM, 320GB disk', 16, 32768, 320)
ON CONFLICT (name) DO NOTHING;

-- Keypairs: SSH public keys for VM access (like AWS key pairs)
CREATE TABLE IF NOT EXISTS keypairs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    public_key TEXT NOT NULL,
    fingerprint VARCHAR(255),
    organization_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(name, organization_id)
);

CREATE INDEX IF NOT EXISTS idx_keypairs_org ON keypairs (organization_id);
CREATE INDEX IF NOT EXISTS idx_keypairs_name ON keypairs (name);
