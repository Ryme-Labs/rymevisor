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
	ID        string
	Name      string
	Driver    StorageDriver
	Path      string
	TotalBytes int64
	UsedBytes  int64
	Encrypted bool
	Config    map[string]string
	CreatedAt interface{}
	UpdatedAt interface{}
}

type Volume struct {
	ID        string
	Name      string
	PoolID    string
	SizeBytes int64
	UsedBytes int64
	Status    VolumeStatus
	Encrypted bool
	Labels    map[string]string
	Snapshots []VolumeSnapshot
	CreatedAt interface{}
	UpdatedAt interface{}
}

type VolumeSnapshot struct {
	ID        string
	VolumeID  string
	Name      string
	SizeBytes int64
	Status    string
	CreatedAt interface{}
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
	PoolID  string
	Page    int
	PerPage int
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
	Name      string
	Driver    StorageDriver
	Path      string
	Encrypted bool
	Config    map[string]string
}

type CreateVolumeRequest struct {
	Name      string
	PoolID    string
	SizeBytes int64
	Encrypted bool
	Labels    map[string]string
}
