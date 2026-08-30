package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/internal/ipam"
)



type IPAMRepository struct {
	pool *pgxpool.Pool
}

func NewIPAMRepository(pool *pgxpool.Pool) *IPAMRepository {
	return &IPAMRepository{pool: pool}
}



func (r *IPAMRepository) EnsureDefaultNetwork(ctx context.Context, organizationID string) (string, string, error) {
	if organizationID == "" {
		organizationID = "00000000-0000-0000-0000-000000000000"
	}

	var networkID, cidr string
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, cidr::text FROM private_networks WHERE organization_id = $1::uuid AND name = 'default' LIMIT 1`,
		organizationID,
	).Scan(&networkID, &cidr)
	if err == nil {
		return networkID, cidr, nil
	}

	cidr = "10.0.0.0/16"

	var newNetID string
	err = r.pool.QueryRow(ctx, `
		INSERT INTO private_networks (id, name, organization_id, cidr, type)
		VALUES (uuid_generate_v4(), 'default', $1::uuid, $2::inet, 'private')
		ON CONFLICT DO NOTHING
		RETURNING id::text
	`, organizationID, cidr).Scan(&newNetID)
	if err != nil {

		err2 := r.pool.QueryRow(ctx,
			`SELECT id::text, cidr::text FROM private_networks WHERE organization_id = $1::uuid AND name = 'default' LIMIT 1`,
			organizationID,
		).Scan(&networkID, &cidr)
		if err2 == nil {
			return networkID, cidr, nil
		}
		return "", "", fmt.Errorf("create default network: %w", err)
	}
	networkID = newNetID

	_, err = r.pool.Exec(ctx, `
		INSERT INTO subnets (id, name, network_id, cidr, dhcp_enabled, gateway_ip)
		VALUES (uuid_generate_v4(), 'default', $1::uuid, $2::inet, true, ($2::cidr + 1)::inet)
		ON CONFLICT DO NOTHING
	`, networkID, cidr)
	if err != nil {

	}
	return networkID, cidr, nil
}


func (r *IPAMRepository) AllocateIP(ctx context.Context, networkID string) (allocated string, gateway string, subnetCIDR string, err error) {

	err = r.pool.QueryRow(ctx, `
		SELECT cidr::text, COALESCE(gateway_ip::text, (cidr::cidr + 1)::text)
		FROM subnets WHERE network_id = $1::uuid ORDER BY dhcp_enabled DESC, created_at ASC LIMIT 1
	`, networkID).Scan(&subnetCIDR, &gateway)
	if err != nil {
		return "", "", "", fmt.Errorf("network %s has no subnet: %w", networkID, err)
	}


	rows, err := r.pool.Query(ctx, `
		SELECT ipv4_addresses[1]::text FROM vm_network_interfaces
		WHERE network_id = $1::uuid AND array_length(ipv4_addresses, 1) > 0
	`, networkID)
	if err != nil {
		return "", "", "", fmt.Errorf("query used IPs: %w", err)
	}
	defer rows.Close()
	var used []string
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err == nil {
			used = append(used, ip)
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", "", err
	}


	allocated, gw2, err := ipam.NextAvailableIP(subnetCIDR, used)
	if err != nil {
		return "", "", "", err
	}

	if gateway == "" {
		gateway = gw2
	}
	return allocated, gateway, subnetCIDR, nil
}


