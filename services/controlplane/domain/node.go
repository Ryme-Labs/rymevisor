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
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Address           string            `json:"address"`
	Port              int32             `json:"port"`
	Status            NodeStatus        `json:"status"`
	TotalCPUs         int32             `json:"total_cpus"`
	UsedCPUs          int32             `json:"used_cpus"`
	TotalMemoryMB     int64             `json:"total_memory_mb"`
	UsedMemoryMB      int64             `json:"used_memory_mb"`
	TotalStorageBytes int64             `json:"total_storage_bytes"`
	UsedStorageBytes  int64             `json:"used_storage_bytes"`
	TotalGPUs         int32             `json:"total_gpus"`
	UsedGPUs          int32             `json:"used_gpus"`
	Labels            map[string]string `json:"labels,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	LastHeartbeat     *time.Time        `json:"last_heartbeat,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
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
	Status  string `json:"status"`
	Search  string `json:"search"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
}

type NodeResources struct {
	TotalCPUs         int32 `json:"total_cpus"`
	UsedCPUs          int32 `json:"used_cpus"`
	TotalMemoryMB     int64 `json:"total_memory_mb"`
	UsedMemoryMB      int64 `json:"used_memory_mb"`
	TotalStorageBytes int64 `json:"total_storage_bytes"`
	UsedStorageBytes  int64 `json:"used_storage_bytes"`
	TotalGPUs         int32 `json:"total_gpus"`
	UsedGPUs          int32 `json:"used_gpus"`
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
	Name      string            `json:"name"`
	Address   string            `json:"address"`
	Port      int32             `json:"port"`
	Labels    map[string]string `json:"labels,omitempty"`
	Resources NodeResources     `json:"resources"`
}
