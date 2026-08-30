-- IPAM: ensure subnets have gateway_ip (AWS reserves .1) and floating IP logic fixed

-- Backfill gateway_ip where NULL: gateway = network + 1 (for IPv4)
-- Use inet helper: network + 1
UPDATE subnets
SET gateway_ip = (cidr::cidr + 1)
WHERE gateway_ip IS NULL
  AND family(cidr) = 4;

-- Floating IPs should also have uniqueness per IP (already has UNIQUE, keep)

-- Index to speed IPAM scans for private networks
CREATE INDEX IF NOT EXISTS idx_private_networks_cidr ON private_networks USING gist (cidr inet_ops);
