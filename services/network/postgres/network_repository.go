package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/network/domain"
)

type NetworkRepository struct {
	pool *pgxpool.Pool
}

func NewNetworkRepository(pool *pgxpool.Pool) *NetworkRepository {
	return &NetworkRepository{pool: pool}
}

func (r *NetworkRepository) Create(ctx context.Context, net *domain.PrivateNetwork) error {
	labelsJSON, err := json.Marshal(net.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO private_networks (id, name, organization_id, vpc_id, type, cidr, ipv6_cidr, internet_gateway, labels)
		 VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6::inet, $7::inet, $8, $9)`,
		net.ID, net.Name, net.OrganizationID, net.VpcID, net.Type, net.CIDR, net.IPv6CIDR, net.InternetGateway, labelsJSON,
	)
	return err
}

func (r *NetworkRepository) GetByID(ctx context.Context, id string) (*domain.PrivateNetwork, error) {
	var net domain.PrivateNetwork
	var labelsJSON []byte
	var orgID, vpcID *string

	err := r.pool.QueryRow(ctx,
		`SELECT id, name, organization_id, vpc_id, type, cidr::text, ipv6_cidr::text, internet_gateway, labels, created_at, updated_at
		 FROM private_networks WHERE id = $1`, id,
	).Scan(
		&net.ID, &net.Name, &orgID, &vpcID, &net.Type, &net.CIDR, &net.IPv6CIDR,
		&net.InternetGateway, &labelsJSON, &net.CreatedAt, &net.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if orgID != nil {
		net.OrganizationID = *orgID
	}
	if vpcID != nil {
		net.VpcID = vpcID
	}

	if labelsJSON != nil {
		if err := json.Unmarshal(labelsJSON, &net.Labels); err != nil {
			return nil, fmt.Errorf("unmarshal labels: %w", err)
		}
	}

	subnets, err := r.listSubnets(ctx, id)
	if err != nil {
		return nil, err
	}
	net.Subnets = subnets

	rules, err := r.listFirewallRules(ctx, id)
	if err != nil {
		return nil, err
	}
	net.FirewallRules = rules

	return &net, nil
}

func (r *NetworkRepository) listSubnets(ctx context.Context, networkID string) ([]domain.Subnet, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, network_id, cidr::text, ipv6_cidr::text, dhcp_enabled, gateway_ip::text, created_at
		 FROM subnets WHERE network_id = $1`, networkID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subnets []domain.Subnet
	for rows.Next() {
		var s domain.Subnet
		if err := rows.Scan(&s.ID, &s.Name, &s.NetworkID, &s.CIDR, &s.IPv6CIDR, &s.DHCPEnabled, &s.GatewayIP, &s.CreatedAt); err != nil {
			return nil, err
		}
		subnets = append(subnets, s)
	}
	return subnets, rows.Err()
}

func (r *NetworkRepository) listFirewallRules(ctx context.Context, networkID string) ([]domain.FirewallRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, network_id, priority, action, direction, source_cidrs, destination_cidrs, protocol, port_min, port_max, enabled, created_at
		 FROM firewall_rules WHERE network_id = $1`, networkID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []domain.FirewallRule
	for rows.Next() {
		var rule domain.FirewallRule
		if err := rows.Scan(
			&rule.ID, &rule.Name, &rule.NetworkID, &rule.Priority, &rule.Action, &rule.Direction,
			&rule.SourceCIDRs, &rule.DestinationCIDRs, &rule.Protocol, &rule.PortMin, &rule.PortMax,
			&rule.Enabled, &rule.CreatedAt,
		); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (r *NetworkRepository) List(ctx context.Context, filter domain.NetworkFilter) ([]*domain.PrivateNetwork, int, error) {
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM private_networks`, 
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.PerPage
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, organization_id, vpc_id, type, cidr::text, ipv6_cidr::text, internet_gateway, labels, created_at, updated_at
		 FROM private_networks
		 ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		filter.PerPage, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var networks []*domain.PrivateNetwork
	for rows.Next() {
		net := &domain.PrivateNetwork{}
		var labelsJSON []byte
		var orgID, vpcID *string
		if err := rows.Scan(
			&net.ID, &net.Name, &orgID, &vpcID, &net.Type, &net.CIDR, &net.IPv6CIDR,
			&net.InternetGateway, &labelsJSON, &net.CreatedAt, &net.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		if orgID != nil {
			net.OrganizationID = *orgID
		}
		if vpcID != nil {
			net.VpcID = vpcID
		}
		if labelsJSON != nil {
			_ = json.Unmarshal(labelsJSON, &net.Labels)
		}
		networks = append(networks, net)
	}
	return networks, total, rows.Err()
}

func (r *NetworkRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM private_networks WHERE id = $1`, id)
	return err
}
