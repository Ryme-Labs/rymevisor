package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rymelabs/rymevisor/services/network/domain"
)

type FirewallRepository struct {
	pool *pgxpool.Pool
}

func NewFirewallRepository(pool *pgxpool.Pool) *FirewallRepository {
	return &FirewallRepository{pool: pool}
}

func (r *FirewallRepository) Create(ctx context.Context, rule *domain.FirewallRule) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO firewall_rules (id, name, network_id, priority, action, direction, source_cidrs, destination_cidrs, protocol, port_min, port_max, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		rule.ID, rule.Name, rule.NetworkID, rule.Priority, rule.Action, rule.Direction,
		rule.SourceCIDRs, rule.DestinationCIDRs, rule.Protocol, rule.PortMin, rule.PortMax, rule.Enabled,
	)
	return err
}

func (r *FirewallRepository) GetByID(ctx context.Context, id string) (*domain.FirewallRule, error) {
	var rule domain.FirewallRule
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, network_id, priority, action, direction, source_cidrs, destination_cidrs, protocol, port_min, port_max, enabled, created_at
		 FROM firewall_rules WHERE id = $1`, id,
	).Scan(
		&rule.ID, &rule.Name, &rule.NetworkID, &rule.Priority, &rule.Action, &rule.Direction,
		&rule.SourceCIDRs, &rule.DestinationCIDRs, &rule.Protocol, &rule.PortMin, &rule.PortMax,
		&rule.Enabled, &rule.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &rule, nil
}

func (r *FirewallRepository) ListByNetwork(ctx context.Context, networkID string) ([]*domain.FirewallRule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, network_id, priority, action, direction, source_cidrs, destination_cidrs, protocol, port_min, port_max, enabled, created_at
		 FROM firewall_rules WHERE network_id = $1`, networkID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []*domain.FirewallRule
	for rows.Next() {
		rule := &domain.FirewallRule{}
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

func (r *FirewallRepository) Delete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM firewall_rules WHERE id = $1`, id)
	return err
}
