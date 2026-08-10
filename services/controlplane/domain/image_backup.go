package domain

import (
	"context"
	"time"
)

type ImageType string

const (
	ImageTypeOS          ImageType = "os"
	ImageTypeApplication ImageType = "application"
	ImageTypeSnapshot    ImageType = "snapshot"
)

type ImageStatus string

const (
	ImageStatusReady       ImageStatus = "ready"
	ImageStatusDownloading ImageStatus = "downloading"
	ImageStatusProcessing  ImageStatus = "processing"
	ImageStatusError       ImageStatus = "error"
)

type Image struct {
	ID           string
	Name         string
	Description  string
	OS           string
	OSVersion    string
	Architecture string
	Type         ImageType
	SizeBytes    int64
	Status       ImageStatus
	Checksum     string
	Tags         []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type ImageRepository interface {
	Create(ctx context.Context, img *Image) error
	GetByID(ctx context.Context, id string) (*Image, error)
	List(ctx context.Context, filter ImageFilter) ([]*Image, int, error)
	Delete(ctx context.Context, id string) error
}

type ImageFilter struct {
	OS           string
	Architecture string
	Type         string
	Search       string
	Page         int
	PerPage      int
}

type ImageService interface {
	Upload(ctx context.Context, req *UploadImageRequest) (*Image, string, error)
	GetImage(ctx context.Context, id string) (*Image, error)
	ListImages(ctx context.Context, filter ImageFilter) ([]*Image, int, error)
	DeleteImage(ctx context.Context, id string) error
	ImportFromURL(ctx context.Context, req *ImportImageRequest) (*Image, error)
}

type UploadImageRequest struct {
	Name         string
	Description  string
	OS           string
	OSVersion    string
	Architecture string
	Type         ImageType
	Tags         []string
}

type ImportImageRequest struct {
	URL          string
	Name         string
	OS           string
	OSVersion    string
	Architecture string
}

type BackupStatus string

const (
	BackupStatusCreating  BackupStatus = "creating"
	BackupStatusRunning   BackupStatus = "running"
	BackupStatusCompleted BackupStatus = "completed"
	BackupStatusFailed    BackupStatus = "failed"
	BackupStatusDeleted   BackupStatus = "deleted"
)

type BackupType string

const (
	BackupTypeFull        BackupType = "full"
	BackupTypeIncremental BackupType = "incremental"
)

type Backup struct {
	ID             string
	Name           string
	VMID           string
	OrganizationID string
	Status         BackupStatus
	SizeBytes      int64
	Type           BackupType
	StoragePool    string
	CreatedAt      time.Time
	CompletedAt    *time.Time
}

type BackupRepository interface {
	Create(ctx context.Context, backup *Backup) error
	GetByID(ctx context.Context, id string) (*Backup, error)
	List(ctx context.Context, filter BackupFilter) ([]*Backup, int, error)
	Update(ctx context.Context, backup *Backup) error
	Delete(ctx context.Context, id string) error
}

type BackupFilter struct {
	VMID           string
	OrganizationID string
	Page           int
	PerPage        int
}

type BackupService interface {
	CreateBackup(ctx context.Context, vmID, name string, backupType BackupType) (*Backup, error)
	GetBackup(ctx context.Context, id string) (*Backup, error)
	ListBackups(ctx context.Context, filter BackupFilter) ([]*Backup, int, error)
	DeleteBackup(ctx context.Context, id string) error
	RestoreBackup(ctx context.Context, backupID, vmID string) error
}
