package postgres

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strings"

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
		 VALUES ($1, $2::inet, NULLIF($3, '')::uuid, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid)`,
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
		 FROM floating_ips ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ips []*domain.FloatingIP
	for rows.Next() {
		ip := &domain.FloatingIP{}
		var orgID *string
		if err := rows.Scan(&ip.ID, &ip.IPAddress, &ip.NetworkID, &ip.VMID, &orgID, &ip.CreatedAt); err != nil {
			return nil, err
		}
		if orgID != nil {
			ip.OrganizationID = *orgID
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
	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return "", fmt.Errorf("only IPv4 CIDR supported, got %q", networkCIDR)
	}
	mask := binary.BigEndian.Uint32(ipNet.Mask)
	network := binary.BigEndian.Uint32(ip4)
	broadcast := network | ^mask
	if broadcast-network <= 2 {
		return "", fmt.Errorf("no available IPs in network %s (prefix too small)", networkCIDR)
	}
	gwInt := network + 1


	query := `SELECT ip_address::text FROM floating_ips WHERE ip_address <<= $1::cidr`
	args := []any{networkCIDR}
	if organizationID != "" {
		query += ` AND organization_id = $2::uuid`
		args = append(args, organizationID)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	usedSet := make(map[uint32]bool)
	for rows.Next() {
		var ipAddr string
		if err := rows.Scan(&ipAddr); err != nil {
			return "", err
		}
		ipStr := ipAddr
		if strings.Contains(ipStr, "/") {
			if parsed, _, err := net.ParseCIDR(ipStr); err == nil {
				ipStr = parsed.String()
			}
		}
		if ip := net.ParseIP(ipStr); ip != nil {
			if v4 := ip.To4(); v4 != nil {
				usedSet[binary.BigEndian.Uint32(v4)] = true
			}
		} else if ip, _, err := net.ParseCIDR(ipAddr); err == nil {
			if v4 := ip.To4(); v4 != nil {
				usedSet[binary.BigEndian.Uint32(v4)] = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}


	usedSet[network] = true
	usedSet[gwInt] = true
	usedSet[broadcast] = true


	for candidate := network + 2; candidate < broadcast; candidate++ {
		if usedSet[candidate] {
			continue
		}
		candidateIP := make(net.IP, 4)
		binary.BigEndian.PutUint32(candidateIP, candidate)
		if !ipNet.Contains(candidateIP) {
			continue
		}

		return candidateIP.String(), nil
	}

	return "", fmt.Errorf("no available IPs in network %s", networkCIDR)
}







func (r *FloatingIPRepository) findAvailableIPSQL(ctx context.Context, networkCIDR string, organizationID string) (string, error) {
	_, ipNet, err := net.ParseCIDR(networkCIDR)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR: %w", err)
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return "", fmt.Errorf("only IPv4 CIDR supported, got %q", networkCIDR)
	}
	hostBits := 32 - ones
	if hostBits < 2 {
		return "", fmt.Errorf("no available IPs in network %s", networkCIDR)
	}
	maxHosts := (1 << hostBits) - 2

	query := `
		SELECT host($1::cidr + s)::text
		FROM generate_series(2, $2::int) AS s
		WHERE host($1::cidr + s)::inet NOT IN (SELECT ip_address FROM floating_ips WHERE ip_address <<= $1::cidr`
	args := []any{networkCIDR, maxHosts}
	if organizationID != "" {
		query += ` AND organization_id = $3::uuid`
		args = append(args, organizationID)
	}
	query += `)
		LIMIT 1`
	var ip string
	err = r.pool.QueryRow(ctx, query, args...).Scan(&ip)
	if err != nil {
		return "", fmt.Errorf("no available IPs in network %s: %w", networkCIDR, err)
	}
	return ip, nil
}
