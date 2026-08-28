package domain

import (
	"context"
)

type NetworkType string

const (
	NetworkTypePrivate NetworkType = "private"
	NetworkTypePublic  NetworkType = "public"
)

type PrivateNetwork struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	OrganizationID  string          `json:"organization_id"`
	VpcID           *string         `json:"vpc_id,omitempty"`
	Type            NetworkType     `json:"type"`
	CIDR            string          `json:"cidr"`
	IPv6CIDR        *string         `json:"ipv6_cidr,omitempty"`
	InternetGateway bool            `json:"internet_gateway"`
	Labels          map[string]string `json:"labels,omitempty"`
	Subnets         []Subnet        `json:"subnets,omitempty"`
	FirewallRules   []FirewallRule  `json:"firewall_rules,omitempty"`
	CreatedAt       interface{}     `json:"created_at"`
	UpdatedAt       interface{}     `json:"updated_at"`
}

type Subnet struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	NetworkID   string      `json:"network_id"`
	CIDR        string      `json:"cidr"`
	IPv6CIDR    *string     `json:"ipv6_cidr,omitempty"`
	DHCPEnabled bool        `json:"dhcp_enabled"`
	GatewayIP   *string     `json:"gateway_ip,omitempty"`
	CreatedAt   interface{} `json:"created_at"`
}

type FirewallRule struct {
	ID               string      `json:"id"`
	Name             string      `json:"name"`
	NetworkID        string      `json:"network_id"`
	Priority         int32       `json:"priority"`
	Action           string      `json:"action"`
	Direction        string      `json:"direction"`
	SourceCIDRs      []string    `json:"source_cidrs,omitempty"`
	DestinationCIDRs []string    `json:"destination_cidrs,omitempty"`
	Protocol         string      `json:"protocol"`
	PortMin          *int32      `json:"port_min,omitempty"`
	PortMax          *int32      `json:"port_max,omitempty"`
	Enabled          bool        `json:"enabled"`
	CreatedAt        interface{} `json:"created_at"`
}

type FloatingIP struct {
	ID             string      `json:"id"`
	IPAddress      string      `json:"ip_address"`
	NetworkID      *string     `json:"network_id,omitempty"`
	VMID           *string     `json:"vm_id,omitempty"`
	OrganizationID string      `json:"organization_id"`
	CreatedAt      interface{} `json:"created_at"`
}

type NetworkRepository interface {
	Create(ctx context.Context, net *PrivateNetwork) error
	GetByID(ctx context.Context, id string) (*PrivateNetwork, error)
	List(ctx context.Context, filter NetworkFilter) ([]*PrivateNetwork, int, error)
	Delete(ctx context.Context, id string) error
}

type NetworkFilter struct {
	OrganizationID string `json:"organization_id"`
	Page           int    `json:"page"`
	PerPage        int    `json:"per_page"`
}

type FirewallRepository interface {
	Create(ctx context.Context, rule *FirewallRule) error
	GetByID(ctx context.Context, id string) (*FirewallRule, error)
	ListByNetwork(ctx context.Context, networkID string) ([]*FirewallRule, error)
	Delete(ctx context.Context, id string) error
}

type FloatingIPRepository interface {
	Allocate(ctx context.Context, ip *FloatingIP) error
	Release(ctx context.Context, id string) error
	List(ctx context.Context, organizationID string) ([]*FloatingIP, error)
	FindAvailableIP(ctx context.Context, networkCIDR string, organizationID string) (string, error)
}

type SubnetRepository interface {
	Create(ctx context.Context, subnet *Subnet) error
	GetByID(ctx context.Context, id string) (*Subnet, error)
	ListByNetwork(ctx context.Context, networkID string) ([]*Subnet, error)
	Delete(ctx context.Context, id string) error
}

type NetworkService interface {
	CreateNetwork(ctx context.Context, req *CreateNetworkRequest) (*PrivateNetwork, error)
	GetNetwork(ctx context.Context, id string) (*PrivateNetwork, error)
	ListNetworks(ctx context.Context, filter NetworkFilter) ([]*PrivateNetwork, int, error)
	DeleteNetwork(ctx context.Context, id string) error
	CreateSubnet(ctx context.Context, networkID, name, cidr string, dhcp bool) (*Subnet, error)
	DeleteSubnet(ctx context.Context, id string) error
	CreateFirewallRule(ctx context.Context, req *CreateFirewallRuleRequest) (*FirewallRule, error)
	DeleteFirewallRule(ctx context.Context, id string) error
	AllocateFloatingIP(ctx context.Context, networkID, vmID string) (*FloatingIP, error)
	ReleaseFloatingIP(ctx context.Context, id string) error
	ListFloatingIPs(ctx context.Context) ([]*FloatingIP, error)
}

type CreateNetworkRequest struct {
	Name            string `json:"name"`
	OrganizationID  string `json:"organization_id"`
	CIDR            string `json:"cidr"`
	IPv6CIDR        string `json:"ipv6_cidr"`
	InternetGateway bool   `json:"internet_gateway"`
}

type CreateFirewallRuleRequest struct {
	NetworkID        string   `json:"network_id"`
	Name             string   `json:"name"`
	Priority         int32    `json:"priority"`
	Action           string   `json:"action"`
	Direction        string   `json:"direction"`
	SourceCIDRs      []string `json:"source_cidrs,omitempty"`
	DestinationCIDRs []string `json:"destination_cidrs,omitempty"`
	Protocol         string   `json:"protocol"`
	PortMin          *int32   `json:"port_min,omitempty"`
	PortMax          *int32   `json:"port_max,omitempty"`
}
