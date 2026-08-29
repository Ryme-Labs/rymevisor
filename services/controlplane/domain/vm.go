package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type VMStatus string

const (
	VMStatusCreating     VMStatus = "creating"
	VMStatusRunning      VMStatus = "running"
	VMStatusStopped      VMStatus = "stopped"
	VMStatusPaused       VMStatus = "paused"
	VMStatusRebooting    VMStatus = "rebooting"
	VMStatusShuttingDown VMStatus = "shutting_down"
	VMStatusTerminated   VMStatus = "terminated"
	VMStatusError        VMStatus = "error"
	VMStatusMigrating    VMStatus = "migrating"
	VMStatusSnapshottting VMStatus = "snapshottting"
)

type VirtualMachine struct {
	ID                 string             `json:"id"`
	Name               string             `json:"name"`
	NodeID             *string            `json:"node_id,omitempty"`
	OrganizationID     string             `json:"organization_id"`
	ProjectID          *string            `json:"project_id,omitempty"`
	Status             VMStatus           `json:"status"`
	VCpus              int32              `json:"vcpus"`
	MemoryMB           int64              `json:"memory_mb"`
	CPUModel           string             `json:"cpu_model"`
	MachineType        string             `json:"machine_type"`
	EnableSecureBoot   bool               `json:"enable_secure_boot"`
	EnableTPM          bool               `json:"enable_tpm"`
	Hugepages          bool               `json:"hugepages"`
	CloudInit          string             `json:"cloud_init"`
	SSHKeyID           *string            `json:"ssh_key_id,omitempty"`
	Tags               []string           `json:"tags,omitempty"`
	Metadata           map[string]string  `json:"metadata,omitempty"`
	Labels             map[string]string  `json:"labels,omitempty"`
	Disks              []Disk             `json:"disks,omitempty"`
	NetworkInterfaces  []NetworkInterface `json:"network_interfaces,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

type Disk struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	Type        string `json:"type"`
	StoragePool string `json:"storage_pool"`
	ImageID     string `json:"image_id,omitempty"`
	Boot        bool   `json:"boot"`
	Order       int32  `json:"order"`
}

type NetworkInterface struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	NetworkID     string   `json:"network_id"`
	MACAddress    string   `json:"mac_address"`
	IPv4Addresses []string `json:"ipv4_addresses,omitempty"`
	IPv6Addresses []string `json:"ipv6_addresses,omitempty"`
	IsPrimary     bool     `json:"is_primary"`
}

type Snapshot struct {
	ID          string    `json:"id"`
	VMID        string    `json:"vm_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SizeBytes   int64     `json:"size_bytes"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type VMRepository interface {
	Create(ctx context.Context, vm *VirtualMachine) error
	GetByID(ctx context.Context, id string) (*VirtualMachine, error)
	List(ctx context.Context, filter VMFilter) ([]*VirtualMachine, int, error)
	Update(ctx context.Context, vm *VirtualMachine) error
	Delete(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id string, status VMStatus) error
}

type VMFilter struct {
	OrganizationID string
	ProjectID      string
	NodeID         string
	Status         string
	Search         string
	Page           int
	PerPage        int
	SortBy         string
	SortOrder      string
}

type SnapshotRepository interface {
	Create(ctx context.Context, snap *Snapshot) error
	GetByID(ctx context.Context, id string) (*Snapshot, error)
	ListByVM(ctx context.Context, vmID string) ([]*Snapshot, error)
	Delete(ctx context.Context, id string) error
}

type VMService interface {
	CreateVM(ctx context.Context, req *CreateVMRequest) (*VirtualMachine, error)
	GetVM(ctx context.Context, id string) (*VirtualMachine, error)
	ListVMs(ctx context.Context, filter VMFilter) ([]*VirtualMachine, int, error)
	UpdateVM(ctx context.Context, id string, req *UpdateVMRequest) (*VirtualMachine, error)
	DeleteVM(ctx context.Context, id string, force bool) error
	PowerOn(ctx context.Context, id string) (*VirtualMachine, error)
	PowerOff(ctx context.Context, id string, force bool) (*VirtualMachine, error)
	Reboot(ctx context.Context, id string, force bool) (*VirtualMachine, error)
	Resize(ctx context.Context, id string, vcpus int32, memoryMB int64) (*VirtualMachine, error)
	Snapshot(ctx context.Context, vmID, name, description string) (*Snapshot, error)
	Clone(ctx context.Context, id string, name string, nodeID string, linked bool) (*VirtualMachine, error)
	RestoreSnapshot(ctx context.Context, snapshotID string) (*VirtualMachine, error)
}

type CreateVMRequest struct {
	Name               string                      `json:"name"`
	NodeID             string                      `json:"node_id"`
	FlavorID           string                      `json:"flavor_id,omitempty"`
	Flavor             string                      `json:"flavor,omitempty"` // alias name like "small", "medium"
	VCpus              int32                       `json:"vcpus"`
	MemoryMB           int64                       `json:"memory_mb"`
	CPUModel           string                      `json:"cpu_model"`
	MachineType        string                      `json:"machine_type"`
	EnableSecureBoot   bool                        `json:"enable_secure_boot"`
	EnableTPM          bool                        `json:"enable_tpm"`
	Hugepages          bool                        `json:"hugepages"`
	CloudInit          string                      `json:"cloud_init"`
	KeypairID          string                      `json:"keypair_id,omitempty"`
	Keypair            string                      `json:"keypair,omitempty"` // name
	Disks              []CreateDiskRequest         `json:"disks"`
	NetworkInterfaces  []CreateNetworkInterfaceRequest `json:"network_interfaces"`
	Tags               []string                    `json:"tags"`
	Metadata           map[string]string           `json:"metadata"`
	Labels             map[string]string           `json:"labels"`
}

type CreateDiskRequest struct {
	Name        string `json:"name"`
	SizeBytes   int64  `json:"size_bytes"`
	Type        string `json:"type"`
	StoragePool string `json:"storage_pool"`
	ImageID     string `json:"image_id,omitempty"`
	Image       string `json:"image,omitempty"` // alias: ubuntu, ubuntu-22.04, debian-12, etc.
}

type CreateNetworkInterfaceRequest struct {
	NetworkID string `json:"network_id"`
	IsPrimary bool   `json:"is_primary"`
}

type UpdateVMRequest struct {
	Name     string            `json:"name"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
}

func NewVMID() string {
	return uuid.New().String()
}
