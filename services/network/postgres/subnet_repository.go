package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/network/domain"
)

type SubnetRepository struct {
	pool *pgxpool.Pool
}

func NewSubnetRepository(pool *pgxpool.Pool) *SubnetRepository {
	return &SubnetRepository{pool: pool}
}

func (r *SubnetRepository) Create(ctx context.Context, subnet *domain.Subnet) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO subnets (id, name, network_id, cidr, ipv6_cidr, dhcp_enabled, gateway_ip)
		 VALUES ($1, $2, $3, $4::inet, $5::inet, $6, $7::inet)`,
		subnet.ID, subnet.Name, subnet.NetworkID, subnet.CIDR, subnet.IPv6CIDR, subnet.DHCPEnabled, subnet.GatewayIP,
	)
	return err
}

func (r *SubnetRepository) GetByID(ctx context.Context, id string) (*domain.Subnet, error) {
	var s domain.Subnet
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, network_id, cidr::text, ipv6_cidr::text, dhcp_enabled, gateway_ip::text, created_at
		 FROM subnets WHERE id = $1`, id,
	).Scan(&s.ID, &s.Name, &s.NetworkID, &s.CIDR, &s.IPv6CIDR, &s.DHCPEnabled, &s.GatewayIP, &s.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

func (r *SubnetRepository) ListByNetwork(ctx context.Context, networkID string) ([]*domain.Subnet, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, network_id, cidr::text, ipv6_cidr::text, dhcp_enabled, gateway_ip::text, created_at
		 FROM subnets WHERE network_id = $1`, networkID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subnets []*domain.Subnet
	for rows.Next() {
		s := &domain.Subnet{}
		if err := rows.Scan(&s.ID, &s.Name, &s.NetworkID, &s.CIDR, &s.IPv6CIDR, &s.DHCPEnabled, &s.GatewayIP, &s.CreatedAt); err != nil {
			return nil, err
		}
		subnets = append(subnets, s)
	}
	return subnets, rows.Err()
}

func (r *SubnetRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM subnets WHERE id = $1`, id)
	return err
}
