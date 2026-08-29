package domain

import (
	"context"
	"time"
)

type Flavor struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	VCpus       int32     `json:"vcpus"`
	MemoryMB    int64     `json:"memory_mb"`
	DiskGB      int64     `json:"disk_gb"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type FlavorRepository interface {
	Create(ctx context.Context, f *Flavor) error
	GetByID(ctx context.Context, id string) (*Flavor, error)
	GetByName(ctx context.Context, name string) (*Flavor, error)
	List(ctx context.Context) ([]*Flavor, error)
	Delete(ctx context.Context, id string) error
}

type CreateFlavorRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	VCpus       int32  `json:"vcpus"`
	MemoryMB    int64  `json:"memory_mb"`
	DiskGB      int64  `json:"disk_gb"`
}
