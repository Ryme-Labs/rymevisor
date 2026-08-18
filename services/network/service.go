package network

import (
	"context"
	"fmt"
	stdnet "net"

	"github.com/google/uuid"
	"github.com/rymelabs/rymevisor/services/network/domain"
)

type Service struct {
	networkRepo   domain.NetworkRepository
	subnetRepo    domain.SubnetRepository
	firewallRepo  domain.FirewallRepository
	floatingIPRepo domain.FloatingIPRepository
}

func NewService(
	networkRepo domain.NetworkRepository,
	subnetRepo domain.SubnetRepository,
	firewallRepo domain.FirewallRepository,
	floatingIPRepo domain.FloatingIPRepository,
) *Service {
	return &Service{
		networkRepo:   networkRepo,
		subnetRepo:    subnetRepo,
		firewallRepo:  firewallRepo,
		floatingIPRepo: floatingIPRepo,
	}
}

func (s *Service) CreateNetwork(ctx context.Context, req *domain.CreateNetworkRequest) (*domain.PrivateNetwork, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.OrganizationID == "" {
		return nil, fmt.Errorf("organization_id is required")
	}
	if req.CIDR == "" {
		return nil, fmt.Errorf("cidr is required")
	}

	_, _, err := stdnet.ParseCIDR(req.CIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	network := &domain.PrivateNetwork{
		ID:               uuid.New().String(),
		Name:             req.Name,
		OrganizationID:   req.OrganizationID,
		Type:             domain.NetworkTypePrivate,
		CIDR:             req.CIDR,
		InternetGateway:  req.InternetGateway,
		Labels:           make(map[string]string),
		Subnets:          make([]domain.Subnet, 0),
		FirewallRules:    make([]domain.FirewallRule, 0),
	}

	if req.IPv6CIDR != "" {
		if _, _, err := stdnet.ParseCIDR(req.IPv6CIDR); err != nil {
			return nil, fmt.Errorf("invalid IPv6 CIDR: %w", err)
		}
		network.IPv6CIDR = &req.IPv6CIDR
	}

	if err := s.networkRepo.Create(ctx, network); err != nil {
		return nil, fmt.Errorf("create network: %w", err)
	}

	defaultSubnet := &domain.Subnet{
		ID:          uuid.New().String(),
		Name:        req.Name + "-default",
		NetworkID:   network.ID,
		CIDR:        req.CIDR,
		DHCPEnabled: true,
	}
	if err := s.subnetRepo.Create(ctx, defaultSubnet); err != nil {
		return nil, fmt.Errorf("create default subnet: %w", err)
	}

	network.Subnets = []domain.Subnet{*defaultSubnet}
	return network, nil
}

func (s *Service) GetNetwork(ctx context.Context, id string) (*domain.PrivateNetwork, error) {
	network, err := s.networkRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		return nil, fmt.Errorf("network not found")
	}
	return network, nil
}

func (s *Service) ListNetworks(ctx context.Context, filter domain.NetworkFilter) ([]*domain.PrivateNetwork, int, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 {
		filter.PerPage = 20
	}
	return s.networkRepo.List(ctx, filter)
}

func (s *Service) DeleteNetwork(ctx context.Context, id string) error {
	network, err := s.networkRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		return fmt.Errorf("network not found")
	}

	subnets, err := s.subnetRepo.ListByNetwork(ctx, id)
	if err != nil {
		return fmt.Errorf("list subnets: %w", err)
	}
	for _, sub := range subnets {
		if sub.ID != "" {
			_ = s.subnetRepo.Delete(ctx, sub.ID)
		}
	}

	return s.networkRepo.Delete(ctx, id)
}

func (s *Service) CreateSubnet(ctx context.Context, networkID, name, cidr string, dhcp bool) (*domain.Subnet, error) {
	if networkID == "" {
		return nil, fmt.Errorf("network_id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if cidr == "" {
		return nil, fmt.Errorf("cidr is required")
	}

	parentNet, err := s.networkRepo.GetByID(ctx, networkID)
	if err != nil {
		return nil, fmt.Errorf("get network: %w", err)
	}
	if parentNet == nil {
		return nil, fmt.Errorf("network not found")
	}

	_, subnetNet, err := stdnet.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	_, parentIPNet, err := stdnet.ParseCIDR(parentNet.CIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid parent CIDR: %w", err)
	}

	if !parentIPNet.Contains(subnetNet.IP) {
		return nil, fmt.Errorf("subnet CIDR must be within network CIDR")
	}

	subnet := &domain.Subnet{
		ID:          uuid.New().String(),
		Name:        name,
		NetworkID:   networkID,
		CIDR:        cidr,
		DHCPEnabled: dhcp,
	}

	if err := s.subnetRepo.Create(ctx, subnet); err != nil {
		return nil, fmt.Errorf("create subnet: %w", err)
	}

	return subnet, nil
}

func (s *Service) DeleteSubnet(ctx context.Context, id string) error {
	subnet, err := s.subnetRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get subnet: %w", err)
	}
	if subnet == nil {
		return fmt.Errorf("subnet not found")
	}
	return s.subnetRepo.Delete(ctx, id)
}

func (s *Service) CreateFirewallRule(ctx context.Context, req *domain.CreateFirewallRuleRequest) (*domain.FirewallRule, error) {
	if req.NetworkID == "" {
		return nil, fmt.Errorf("network_id is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Action == "" {
		req.Action = "allow"
	}
	if req.Direction == "" {
		req.Direction = "ingress"
	}
	if req.Protocol == "" {
		req.Protocol = "any"
	}

	if req.PortMin != nil && req.PortMax != nil {
		if *req.PortMin < 1 || *req.PortMax > 65535 {
			return nil, fmt.Errorf("port range must be 1-65535")
		}
		if *req.PortMin > *req.PortMax {
			return nil, fmt.Errorf("port_min must be <= port_max")
		}
	}

	rule := &domain.FirewallRule{
		ID:              uuid.New().String(),
		Name:            req.Name,
		NetworkID:       req.NetworkID,
		Priority:        req.Priority,
		Action:          req.Action,
		Direction:       req.Direction,
		SourceCIDRs:     req.SourceCIDRs,
		DestinationCIDRs: req.DestinationCIDRs,
		Protocol:        req.Protocol,
		PortMin:         req.PortMin,
		PortMax:         req.PortMax,
		Enabled:         true,
	}

	if rule.Priority == 0 {
		rule.Priority = 100
	}

	if err := s.firewallRepo.Create(ctx, rule); err != nil {
		return nil, fmt.Errorf("create firewall rule: %w", err)
	}

	return rule, nil
}

func (s *Service) DeleteFirewallRule(ctx context.Context, id string) error {
	rule, err := s.firewallRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get firewall rule: %w", err)
	}
	if rule == nil {
		return fmt.Errorf("firewall rule not found")
	}
	return s.firewallRepo.Delete(ctx, id)
}

func (s *Service) AllocateFloatingIP(ctx context.Context, networkID, vmID string) (*domain.FloatingIP, error) {
	if networkID == "" {
		return nil, fmt.Errorf("network_id is required")
	}

	network, err := s.networkRepo.GetByID(ctx, networkID)
	if err != nil {
		return nil, fmt.Errorf("get network: %w", err)
	}
	if network == nil {
		return nil, fmt.Errorf("network not found")
	}

	ipAddr, err := s.floatingIPRepo.FindAvailableIP(ctx, network.CIDR, network.OrganizationID)
	if err != nil {
		return nil, fmt.Errorf("find available IP: %w", err)
	}

	fip := &domain.FloatingIP{
		ID:            uuid.New().String(),
		IPAddress:     ipAddr,
		NetworkID:     &networkID,
		VMID:          &vmID,
		OrganizationID: network.OrganizationID,
	}

	if err := s.floatingIPRepo.Allocate(ctx, fip); err != nil {
		return nil, fmt.Errorf("allocate floating IP: %w", err)
	}

	return fip, nil
}

func (s *Service) ReleaseFloatingIP(ctx context.Context, id string) error {
	return s.floatingIPRepo.Release(ctx, id)
}
