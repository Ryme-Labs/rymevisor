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
	ID                 string
	Name               string
	NodeID             *string
	OrganizationID     string
	ProjectID          *string
	Status             VMStatus
	VCpus              int32
	MemoryMB           int64
	CPUModel           string
	MachineType        string
	EnableSecureBoot   bool
	EnableTPM          bool
	Hugepages          bool
	CloudInit          string
	SSHKeyID           *string
	Tags               []string
	Metadata           map[string]string
	Labels             map[string]string
	Disks              []Disk
	NetworkInterfaces  []NetworkInterface
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Disk struct {
	ID          string
	Name        string
	SizeBytes   int64
	Type        string
	StoragePool string
	Boot        bool
	Order       int32
}

type NetworkInterface struct {
	ID            string
	Name          string
	NetworkID     string
	MACAddress    string
	IPv4Addresses []string
	IPv6Addresses []string
	IsPrimary     bool
}

type Snapshot struct {
	ID          string
	VMID        string
	Name        string
	Description string
	SizeBytes   int64
	Status      string
	CreatedAt   time.Time
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
	Name               string
	NodeID             string
	VCpus              int32
	MemoryMB           int64
	CPUModel           string
	MachineType        string
	EnableSecureBoot   bool
	EnableTPM          bool
	Hugepages          bool
	CloudInit          string
	Disks              []CreateDiskRequest
	NetworkInterfaces  []CreateNetworkInterfaceRequest
	Tags               []string
	Metadata           map[string]string
	Labels             map[string]string
}

type CreateDiskRequest struct {
	Name        string
	SizeBytes   int64
	Type        string
	StoragePool string
}

type CreateNetworkInterfaceRequest struct {
	NetworkID string
	IsPrimary bool
}

type UpdateVMRequest struct {
	Name     string
	Metadata map[string]string
	Labels   map[string]string
	Tags     []string
}

func NewVMID() string {
	return uuid.New().String()
}
