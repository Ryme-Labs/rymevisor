-- Networking service schema

CREATE TYPE network_type AS ENUM ('private', 'public');
CREATE TYPE firewall_action AS ENUM ('allow', 'deny');
CREATE TYPE firewall_direction AS ENUM ('ingress', 'egress');

CREATE TABLE private_networks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    organization_id UUID NOT NULL,
    vpc_id UUID,
    type network_type NOT NULL DEFAULT 'private',
    cidr INET NOT NULL,
    ipv6_cidr INET,
    internet_gateway BOOLEAN NOT NULL DEFAULT false,
    labels JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_networks_organization ON private_networks (organization_id);
CREATE INDEX idx_networks_vpc ON private_networks (vpc_id);

CREATE TABLE subnets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    network_id UUID NOT NULL REFERENCES private_networks(id) ON DELETE CASCADE,
    cidr INET NOT NULL,
    ipv6_cidr INET,
    dhcp_enabled BOOLEAN NOT NULL DEFAULT true,
    gateway_ip INET,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_subnets_network ON subnets (network_id);

CREATE TABLE firewall_rules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    network_id UUID NOT NULL REFERENCES private_networks(id) ON DELETE CASCADE,
    priority INTEGER NOT NULL DEFAULT 100,
    action firewall_action NOT NULL DEFAULT 'allow',
    direction firewall_direction NOT NULL DEFAULT 'ingress',
    source_cidrs INET[],
    destination_cidrs INET[],
    protocol VARCHAR(10) NOT NULL DEFAULT 'any',
    port_min INTEGER,
    port_max INTEGER,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_firewall_rules_network ON firewall_rules (network_id);
CREATE INDEX idx_firewall_rules_priority ON firewall_rules (priority);

CREATE TABLE floating_ips (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ip_address INET NOT NULL UNIQUE,
    network_id UUID REFERENCES private_networks(id) ON DELETE SET NULL,
    vm_id UUID,
    organization_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_floating_ips_organization ON floating_ips (organization_id);
CREATE INDEX idx_floating_ips_vm ON floating_ips (vm_id);
