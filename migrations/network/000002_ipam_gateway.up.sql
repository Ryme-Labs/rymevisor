
UPDATE subnets
SET gateway_ip = (cidr::cidr + 1)
WHERE gateway_ip IS NULL
  AND family(cidr) = 4;


CREATE INDEX IF NOT EXISTS idx_private_networks_cidr ON private_networks USING gist (cidr inet_ops);
