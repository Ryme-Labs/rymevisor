
CREATE UNIQUE INDEX IF NOT EXISTS idx_vm_nics_unique_ip_per_network
ON vm_network_interfaces (network_id, (ipv4_addresses[1]))
WHERE network_id IS NOT NULL AND array_length(ipv4_addresses, 1) > 0;

CREATE UNIQUE INDEX IF NOT EXISTS idx_vm_nics_unique_mac
ON vm_network_interfaces (mac_address)
WHERE mac_address IS NOT NULL AND mac_address <> '';

CREATE INDEX IF NOT EXISTS idx_vm_nics_network ON vm_network_interfaces (network_id);
CREATE INDEX IF NOT EXISTS idx_vm_nics_vm_network ON vm_network_interfaces (vm_id, network_id);
