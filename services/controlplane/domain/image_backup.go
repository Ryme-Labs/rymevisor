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
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Description  string      `json:"description"`
	OS           string      `json:"os"`
	OSVersion    string      `json:"os_version"`
	Architecture string      `json:"architecture"`
	Type         ImageType   `json:"type"`
	SizeBytes    int64       `json:"size_bytes"`
	Status       ImageStatus `json:"status"`
	Checksum     string      `json:"checksum"`
	SourceURL    string      `json:"source_url,omitempty"`
	Tags         []string    `json:"tags,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

type ImageRepository interface {
	Create(ctx context.Context, img *Image) error
	GetByID(ctx context.Context, id string) (*Image, error)
	GetByName(ctx context.Context, name string) (*Image, error)
	List(ctx context.Context, filter ImageFilter) ([]*Image, int, error)
	Update(ctx context.Context, img *Image) error
	UpdateStatus(ctx context.Context, id string, status ImageStatus, sizeBytes int64, checksum string) error
	Delete(ctx context.Context, id string) error
}

type ImageFilter struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Type         string `json:"type"`
	Search       string `json:"search"`
	Page         int    `json:"page"`
	PerPage      int    `json:"per_page"`
}

type ImageService interface {
	Upload(ctx context.Context, req *UploadImageRequest) (*Image, string, error)
	GetImage(ctx context.Context, id string) (*Image, error)
	ListImages(ctx context.Context, filter ImageFilter) ([]*Image, int, error)
	DeleteImage(ctx context.Context, id string) error
	ImportFromURL(ctx context.Context, req *ImportImageRequest) (*Image, error)
	PullOfficialImage(ctx context.Context, os, version, arch string) (*Image, error)
	ListOfficialImages(ctx context.Context) ([]OfficialImage, error)
	GetOfficialImage(ctx context.Context, os, version, arch string) (*OfficialImage, error)
}

type OfficialImage struct {
	Name         string `json:"name"`
	OS           string `json:"os"`
	OSVersion    string `json:"os_version"`
	Architecture string `json:"architecture"`
	URL          string `json:"url"`
	Checksum     string `json:"checksum,omitempty"`
	SizeBytes    int64  `json:"size_bytes"`
	Description  string `json:"description"`
}

type PullImageRequest struct {
	OS           string `json:"os"`
	OSVersion    string `json:"os_version"`
	Architecture string `json:"architecture"`
	Name         string `json:"name,omitempty"`
}

type UploadImageRequest struct {
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	OS           string    `json:"os"`
	OSVersion    string    `json:"os_version"`
	Architecture string    `json:"architecture"`
	Type         ImageType `json:"type"`
	Tags         []string  `json:"tags,omitempty"`
}

type ImportImageRequest struct {
	URL          string `json:"url"`
	Name         string `json:"name"`
	OS           string `json:"os"`
	OSVersion    string `json:"os_version"`
	Architecture string `json:"architecture"`
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
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	VMID           string       `json:"vm_id"`
	OrganizationID string       `json:"organization_id"`
	Status         BackupStatus `json:"status"`
	SizeBytes      int64        `json:"size_bytes"`
	Type           BackupType   `json:"type"`
	StoragePool    string       `json:"storage_pool"`
	CreatedAt      time.Time    `json:"created_at"`
	CompletedAt    *time.Time   `json:"completed_at,omitempty"`
}

type BackupRepository interface {
	Create(ctx context.Context, backup *Backup) error
	GetByID(ctx context.Context, id string) (*Backup, error)
	List(ctx context.Context, filter BackupFilter) ([]*Backup, int, error)
	Update(ctx context.Context, backup *Backup) error
	Delete(ctx context.Context, id string) error
}

type BackupFilter struct {
	VMID           string `json:"vm_id"`
	OrganizationID string `json:"organization_id"`
	Page           int    `json:"page"`
	PerPage        int    `json:"per_page"`
}

type BackupService interface {
	CreateBackup(ctx context.Context, vmID, name string, backupType BackupType) (*Backup, error)
	GetBackup(ctx context.Context, id string) (*Backup, error)
	ListBackups(ctx context.Context, filter BackupFilter) ([]*Backup, int, error)
	DeleteBackup(ctx context.Context, id string) error
	RestoreBackup(ctx context.Context, backupID, vmID string) error
}
