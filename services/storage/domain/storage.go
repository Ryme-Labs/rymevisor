package domain

import (
	"context"
)

type StorageDriver string

const (
	StorageDriverQCOW2   StorageDriver = "qcow2"
	StorageDriverLVMThin StorageDriver = "lvm_thin"
	StorageDriverZFS     StorageDriver = "zfs"
	StorageDriverNFS     StorageDriver = "nfs"
	StorageDriverCeph    StorageDriver = "ceph"
)

type VolumeStatus string

const (
	VolumeStatusAvailable VolumeStatus = "available"
	VolumeStatusInUse     VolumeStatus = "in_use"
	VolumeStatusCreating  VolumeStatus = "creating"
	VolumeStatusDeleting  VolumeStatus = "deleting"
	VolumeStatusError     VolumeStatus = "error"
)

type StoragePool struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     StorageDriver     `json:"driver"`
	Path       string            `json:"path"`
	TotalBytes int64             `json:"total_bytes"`
	UsedBytes  int64             `json:"used_bytes"`
	Encrypted  bool              `json:"encrypted"`
	Config     map[string]string `json:"config,omitempty"`
	CreatedAt  interface{}       `json:"created_at"`
	UpdatedAt  interface{}       `json:"updated_at"`
}

type Volume struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	PoolID    string            `json:"pool_id"`
	SizeBytes int64             `json:"size_bytes"`
	UsedBytes int64             `json:"used_bytes"`
	Status    VolumeStatus      `json:"status"`
	Encrypted bool              `json:"encrypted"`
	Labels    map[string]string `json:"labels,omitempty"`
	Snapshots []VolumeSnapshot  `json:"snapshots,omitempty"`
	CreatedAt interface{}       `json:"created_at"`
	UpdatedAt interface{}       `json:"updated_at"`
}

type VolumeSnapshot struct {
	ID        string      `json:"id"`
	VolumeID  string      `json:"volume_id"`
	Name      string      `json:"name"`
	SizeBytes int64       `json:"size_bytes"`
	Status    string      `json:"status"`
	CreatedAt interface{} `json:"created_at"`
}

type StoragePoolRepository interface {
	Create(ctx context.Context, pool *StoragePool) error
	GetByID(ctx context.Context, id string) (*StoragePool, error)
	List(ctx context.Context) ([]*StoragePool, error)
}

type VolumeRepository interface {
	Create(ctx context.Context, vol *Volume) error
	GetByID(ctx context.Context, id string) (*Volume, error)
	List(ctx context.Context, filter VolumeFilter) ([]*Volume, int, error)
	Update(ctx context.Context, vol *Volume) error
	Delete(ctx context.Context, id string) error
}

type VolumeFilter struct {
	PoolID  string `json:"pool_id"`
	Page    int    `json:"page"`
	PerPage int    `json:"per_page"`
}

type SnapshotRepository interface {
	Create(ctx context.Context, snap *VolumeSnapshot) error
	GetByID(ctx context.Context, id string) (*VolumeSnapshot, error)
	ListByVolume(ctx context.Context, volumeID string) ([]*VolumeSnapshot, error)
	Delete(ctx context.Context, id string) error
}

type StorageService interface {
	CreatePool(ctx context.Context, req *CreatePoolRequest) (*StoragePool, error)
	GetPool(ctx context.Context, id string) (*StoragePool, error)
	ListPools(ctx context.Context) ([]*StoragePool, error)
	CreateVolume(ctx context.Context, req *CreateVolumeRequest) (*Volume, error)
	GetVolume(ctx context.Context, id string) (*Volume, error)
	ListVolumes(ctx context.Context, filter VolumeFilter) ([]*Volume, int, error)
	DeleteVolume(ctx context.Context, id string, force bool) error
	ResizeVolume(ctx context.Context, id string, sizeBytes int64) (*Volume, error)
	CloneVolume(ctx context.Context, id string, name string) (*Volume, error)
	CreateSnapshot(ctx context.Context, volumeID, name string) (*VolumeSnapshot, error)
	DeleteSnapshot(ctx context.Context, id string) error
	RestoreSnapshot(ctx context.Context, snapshotID string) (*Volume, error)
}

type CreatePoolRequest struct {
	Name      string            `json:"name"`
	Driver    StorageDriver     `json:"driver"`
	Path      string            `json:"path"`
	Encrypted bool              `json:"encrypted"`
	Config    map[string]string `json:"config,omitempty"`
}

type CreateVolumeRequest struct {
	Name      string            `json:"name"`
	PoolID    string            `json:"pool_id"`
	SizeBytes int64             `json:"size_bytes"`
	Encrypted bool              `json:"encrypted"`
	Labels    map[string]string `json:"labels,omitempty"`
}
