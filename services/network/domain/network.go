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
	ID               string
	Name             string
	OrganizationID   string
	VpcID            *string
	Type             NetworkType
	CIDR             string
	IPv6CIDR         *string
	InternetGateway  bool
	Labels           map[string]string
	Subnets          []Subnet
	FirewallRules    []FirewallRule
	CreatedAt        interface{}
	UpdatedAt        interface{}
}

type Subnet struct {
	ID          string
	Name        string
	NetworkID   string
	CIDR        string
	IPv6CIDR    *string
	DHCPEnabled bool
	GatewayIP   *string
	CreatedAt   interface{}
}

type FirewallRule struct {
	ID              string
	Name            string
	NetworkID       string
	Priority        int32
	Action          string
	Direction       string
	SourceCIDRs     []string
	DestinationCIDRs []string
	Protocol        string
	PortMin         *int32
	PortMax         *int32
	Enabled         bool
	CreatedAt       interface{}
}

type FloatingIP struct {
	ID            string
	IPAddress     string
	NetworkID     *string
	VMID          *string
	OrganizationID string
	CreatedAt     interface{}
}

type NetworkRepository interface {
	Create(ctx context.Context, net *PrivateNetwork) error
	GetByID(ctx context.Context, id string) (*PrivateNetwork, error)
	List(ctx context.Context, filter NetworkFilter) ([]*PrivateNetwork, int, error)
	Delete(ctx context.Context, id string) error
}

type NetworkFilter struct {
	OrganizationID string
	Page           int
	PerPage        int
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
}

type CreateNetworkRequest struct {
	Name            string
	OrganizationID  string
	CIDR            string
	IPv6CIDR        string
	InternetGateway bool
}

type CreateFirewallRuleRequest struct {
	NetworkID        string
	Name             string
	Priority         int32
	Action           string
	Direction        string
	SourceCIDRs      []string
	DestinationCIDRs []string
	Protocol         string
	PortMin          *int32
	PortMax          *int32
}
