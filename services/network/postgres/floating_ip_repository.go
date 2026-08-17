package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/network/domain"
)

type FloatingIPRepository struct {
	pool *pgxpool.Pool
}

func NewFloatingIPRepository(pool *pgxpool.Pool) *FloatingIPRepository {
	return &FloatingIPRepository{pool: pool}
}

func (r *FloatingIPRepository) Allocate(ctx context.Context, ip *domain.FloatingIP) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO floating_ips (id, ip_address, network_id, vm_id, organization_id)
		 VALUES ($1, $2::inet, $3, $4, $5)`,
		ip.ID, ip.IPAddress, ip.NetworkID, ip.VMID, ip.OrganizationID,
	)
	return err
}

func (r *FloatingIPRepository) Release(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM floating_ips WHERE id = $1`, id)
	return err
}

func (r *FloatingIPRepository) List(ctx context.Context, organizationID string) ([]*domain.FloatingIP, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, ip_address::text, network_id, vm_id, organization_id, created_at
		 FROM floating_ips WHERE organization_id = $1`, organizationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ips []*domain.FloatingIP
	for rows.Next() {
		ip := &domain.FloatingIP{}
		if err := rows.Scan(&ip.ID, &ip.IPAddress, &ip.NetworkID, &ip.VMID, &ip.OrganizationID, &ip.CreatedAt); err != nil {
			return nil, err
		}
		ips = append(ips, ip)
	}
	return ips, rows.Err()
}

func (r *FloatingIPRepository) FindAvailableIP(ctx context.Context, networkCIDR string, organizationID string) (string, error) {
	_, ipNet, err := net.ParseCIDR(networkCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR: %w", err)
	}

	usedIPs := make(map[string]bool)
	rows, err := r.pool.Query(ctx,
		`SELECT ip_address::text FROM floating_ips WHERE organization_id = $1`, organizationID,
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		var ipAddr string
		if err := rows.Scan(&ipAddr); err != nil {
			return "", err
		}
		if ip, _, err := net.ParseCIDR(ipAddr + "/32"); err == nil {
			usedIPs[ip.String()] = true
		} else if ip := net.ParseIP(ipAddr); ip != nil {
			usedIPs[ip.String()] = true
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	ip := ipNet.IP.To4()
	if ip == nil {
		ip = ipNet.IP.To16()
	}
	ip = ip.To4()

	startIP := make(net.IP, len(ip))
	copy(startIP, ip)

	for i := int(startIP[len(startIP)-1]); i < 255; i++ {
		candidate := make(net.IP, len(startIP))
		copy(candidate, startIP)
		candidate[len(candidate)-1] = byte(i)

		if !ipNet.Contains(candidate) {
			continue
		}
		if i == 0 || i == 255 {
			continue
		}
		if candidate.String() == ipNet.IP.String() {
			continue
		}
		gw := make(net.IP, len(ipNet.IP.To4()))
		copy(gw, ipNet.IP.To4())
		gw[len(gw)-1] = byte(1)
		if candidate.Equal(gw) {
			continue
		}

		if !usedIPs[candidate.String()] {
			var count int
			err := r.pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM floating_ips WHERE ip_address = $1::inet`, candidate.String(),
			).Scan(&count)
			if err != nil && err != sql.ErrNoRows {
				return "", err
			}
			if count == 0 {
				return candidate.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no available IPs in network %s", networkCIDR)
}
