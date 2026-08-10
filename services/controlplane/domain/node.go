package domain

import (
	"context"
	"time"
)

type NodeStatus string

const (
	NodeStatusOnline      NodeStatus = "online"
	NodeStatusOffline     NodeStatus = "offline"
	NodeStatusDraining    NodeStatus = "draining"
	NodeStatusMaintenance NodeStatus = "maintenance"
	NodeStatusError       NodeStatus = "error"
)

type Node struct {
	ID              string
	Name            string
	Address         string
	Port            int32
	Status          NodeStatus
	TotalCPUs       int32
	UsedCPUs        int32
	TotalMemoryMB   int64
	UsedMemoryMB    int64
	TotalStorageBytes int64
	UsedStorageBytes int64
	TotalGPUs       int32
	UsedGPUs        int32
	Labels          map[string]string
	Metadata        map[string]string
	LastHeartbeat   *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type NodeRepository interface {
	Create(ctx context.Context, node *Node) error
	GetByID(ctx context.Context, id string) (*Node, error)
	GetByName(ctx context.Context, name string) (*Node, error)
	List(ctx context.Context, filter NodeFilter) ([]*Node, int, error)
	Update(ctx context.Context, node *Node) error
	Delete(ctx context.Context, id string) error
	UpdateStatus(ctx context.Context, id string, status NodeStatus) error
	UpdateHeartbeat(ctx context.Context, id string, resources NodeResources) error
}

type NodeFilter struct {
	Status string
	Search string
	Page   int
	PerPage int
}

type NodeResources struct {
	TotalCPUs       int32
	UsedCPUs        int32
	TotalMemoryMB   int64
	UsedMemoryMB    int64
	TotalStorageBytes int64
	UsedStorageBytes int64
	TotalGPUs       int32
	UsedGPUs        int32
}

type NodeService interface {
	Register(ctx context.Context, req *RegisterNodeRequest) (*Node, error)
	GetNode(ctx context.Context, id string) (*Node, error)
	ListNodes(ctx context.Context, filter NodeFilter) ([]*Node, int, error)
	UpdateNode(ctx context.Context, id string, labels map[string]string) (*Node, error)
	Drain(ctx context.Context, id string, timeout int32) error
	Heartbeat(ctx context.Context, nodeID string, resources NodeResources) error
}

type RegisterNodeRequest struct {
	Name      string
	Address   string
	Port      int32
	Labels    map[string]string
	Resources NodeResources
}
