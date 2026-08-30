-- AWS/GCP-like VPC isolation: per-network IP uniqueness & MAC uniqueness

-- Enforce unique IP per network (prevent two VMs getting same 10.0.0.5)
-- ipv4_addresses is INET[] with single allocated IP (/cidr). Use first element.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vm_nics_unique_ip_per_network
ON vm_network_interfaces (network_id, (ipv4_addresses[1]))
WHERE network_id IS NOT NULL AND array_length(ipv4_addresses, 1) > 0;

-- Global MAC uniqueness (MACs must be unique across host to avoid L2 collision)
CREATE UNIQUE INDEX IF NOT EXISTS idx_vm_nics_unique_mac
ON vm_network_interfaces (mac_address)
WHERE mac_address IS NOT NULL AND mac_address <> '';

-- Helpful indexes for IPAM scans
CREATE INDEX IF NOT EXISTS idx_vm_nics_network ON vm_network_interfaces (network_id);
CREATE INDEX IF NOT EXISTS idx_vm_nics_vm_network ON vm_network_interfaces (vm_id, network_id);
