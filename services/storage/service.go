package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/rymelabs/rymevisor/internal/qcow2"
	"github.com/rymelabs/rymevisor/services/storage/domain"
)

type Service struct {
	poolRepo   domain.StoragePoolRepository
	volumeRepo domain.VolumeRepository
	snapRepo   domain.SnapshotRepository
}

func NewService(
	poolRepo domain.StoragePoolRepository,
	volumeRepo domain.VolumeRepository,
	snapRepo domain.SnapshotRepository,
) *Service {
	return &Service{
		poolRepo:   poolRepo,
		volumeRepo: volumeRepo,
		snapRepo:   snapRepo,
	}
}

func (s *Service) CreatePool(ctx context.Context, req *domain.CreatePoolRequest) (*domain.StoragePool, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if req.Driver == "" {
		req.Driver = domain.StorageDriverQCOW2
	}

	if err := os.MkdirAll(req.Path, 0755); err != nil {
		return nil, fmt.Errorf("create pool directory: %w", err)
	}

	config := req.Config
	if config == nil {
		config = make(map[string]string)
	}

	pool := &domain.StoragePool{
		ID:        uuid.New().String(),
		Name:      req.Name,
		Driver:    req.Driver,
		Path:      req.Path,
		Encrypted: req.Encrypted,
		Config:    config,
	}

	if err := s.poolRepo.Create(ctx, pool); err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	return pool, nil
}

func (s *Service) GetPool(ctx context.Context, id string) (*domain.StoragePool, error) {
	return s.poolRepo.GetByID(ctx, id)
}

func (s *Service) ListPools(ctx context.Context) ([]*domain.StoragePool, error) {
	return s.poolRepo.List(ctx)
}

func (s *Service) CreateVolume(ctx context.Context, req *domain.CreateVolumeRequest) (*domain.Volume, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.PoolID == "" {
		return nil, fmt.Errorf("pool_id is required")
	}
	if req.SizeBytes <= 0 {
		return nil, fmt.Errorf("size_bytes must be positive")
	}

	pool, err := s.poolRepo.GetByID(ctx, req.PoolID)
	if err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}
	if pool == nil {
		return nil, fmt.Errorf("pool not found")
	}

	if pool.UsedBytes+req.SizeBytes > pool.TotalBytes && pool.TotalBytes > 0 {
		return nil, fmt.Errorf("insufficient space in pool")
	}

	labels := req.Labels
	if labels == nil {
		labels = make(map[string]string)
	}

	vol := &domain.Volume{
		ID:        uuid.New().String(),
		Name:      req.Name,
		PoolID:    req.PoolID,
		SizeBytes: req.SizeBytes,
		Status:    domain.VolumeStatusCreating,
		Encrypted: req.Encrypted,
		Labels:    labels,
	}

	if err := s.volumeRepo.Create(ctx, vol); err != nil {
		return nil, fmt.Errorf("create volume: %w", err)
	}

	diskPath := filepath.Join(pool.Path, vol.ID+".qcow2")
	if err := qcow2.Create(ctx, diskPath, req.SizeBytes); err != nil {
		_ = s.volumeRepo.Delete(ctx, vol.ID)
		return nil, err
	}

	vol.Status = domain.VolumeStatusAvailable
	if err := s.volumeRepo.Update(ctx, vol); err != nil {
		return nil, fmt.Errorf("update volume status: %w", err)
	}

	pool.UsedBytes += req.SizeBytes
	_ = s.poolRepo.Update(ctx, pool)

	return vol, nil
}

func (s *Service) GetVolume(ctx context.Context, id string) (*domain.Volume, error) {
	return s.volumeRepo.GetByID(ctx, id)
}

func (s *Service) ListVolumes(ctx context.Context, filter domain.VolumeFilter) ([]*domain.Volume, int, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 {
		filter.PerPage = 20
	}
	return s.volumeRepo.List(ctx, filter)
}

func (s *Service) DeleteVolume(ctx context.Context, id string, force bool) error {
	vol, err := s.volumeRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get volume: %w", err)
	}
	if vol == nil {
		return fmt.Errorf("volume not found")
	}

	if !force && vol.Status == domain.VolumeStatusInUse {
		return fmt.Errorf("volume is in use, use force to delete")
	}

	pool, err := s.poolRepo.GetByID(ctx, vol.PoolID)
	if err == nil && pool != nil {
		diskPath := filepath.Join(pool.Path, vol.ID+".qcow2")
		_ = os.Remove(diskPath)
	}

	return s.volumeRepo.Delete(ctx, id)
}

func (s *Service) ResizeVolume(ctx context.Context, id string, sizeBytes int64) (*domain.Volume, error) {
	if sizeBytes <= 0 {
		return nil, fmt.Errorf("size_bytes must be positive")
	}

	vol, err := s.volumeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get volume: %w", err)
	}
	if vol == nil {
		return nil, fmt.Errorf("volume not found")
	}

	pool, err := s.poolRepo.GetByID(ctx, vol.PoolID)
	if err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	diskPath := filepath.Join(pool.Path, vol.ID+".qcow2")
	if err := qcow2.Resize(ctx, diskPath, sizeBytes); err != nil {
		return nil, err
	}

	if sizeBytes > vol.SizeBytes {
		pool.UsedBytes += (sizeBytes - vol.SizeBytes)
	} else {
		pool.UsedBytes -= (vol.SizeBytes - sizeBytes)
		if pool.UsedBytes < 0 {
			pool.UsedBytes = 0
		}
	}
	_ = s.poolRepo.Update(ctx, pool)

	vol.SizeBytes = sizeBytes
	if err := s.volumeRepo.Update(ctx, vol); err != nil {
		return nil, fmt.Errorf("update volume: %w", err)
	}

	return vol, nil
}

func (s *Service) CloneVolume(ctx context.Context, id string, name string) (*domain.Volume, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	srcVol, err := s.volumeRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get source volume: %w", err)
	}
	if srcVol == nil {
		return nil, fmt.Errorf("source volume not found")
	}

	pool, err := s.poolRepo.GetByID(ctx, srcVol.PoolID)
	if err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	newVol := &domain.Volume{
		ID:        uuid.New().String(),
		Name:      name,
		PoolID:    srcVol.PoolID,
		SizeBytes: srcVol.SizeBytes,
		Status:    domain.VolumeStatusCreating,
		Encrypted: srcVol.Encrypted,
		Labels:    make(map[string]string),
	}

	if err := s.volumeRepo.Create(ctx, newVol); err != nil {
		return nil, fmt.Errorf("create volume: %w", err)
	}

	srcPath := filepath.Join(pool.Path, srcVol.ID+".qcow2")
	dstPath := filepath.Join(pool.Path, newVol.ID+".qcow2")

	if err := qcow2.Clone(ctx, srcPath, dstPath); err != nil {
		_ = s.volumeRepo.Delete(ctx, newVol.ID)
		return nil, err
	}

	newVol.Status = domain.VolumeStatusAvailable
	if err := s.volumeRepo.Update(ctx, newVol); err != nil {
		return nil, fmt.Errorf("update volume status: %w", err)
	}

	pool.UsedBytes += srcVol.SizeBytes
	_ = s.poolRepo.Update(ctx, pool)

	return newVol, nil
}

func (s *Service) CreateSnapshot(ctx context.Context, volumeID, name string) (*domain.VolumeSnapshot, error) {
	if volumeID == "" {
		return nil, fmt.Errorf("volume_id is required")
	}
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	vol, err := s.volumeRepo.GetByID(ctx, volumeID)
	if err != nil {
		return nil, fmt.Errorf("get volume: %w", err)
	}
	if vol == nil {
		return nil, fmt.Errorf("volume not found")
	}

	pool, err := s.poolRepo.GetByID(ctx, vol.PoolID)
	if err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	snap := &domain.VolumeSnapshot{
		ID:       uuid.New().String(),
		VolumeID: volumeID,
		Name:     name,
		Status:   "creating",
	}

	if err := s.snapRepo.Create(ctx, snap); err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}

	diskPath := filepath.Join(pool.Path, vol.ID+".qcow2")
	if err := qcow2.SnapshotCreate(ctx, diskPath, name); err != nil {
		_ = s.snapRepo.Delete(ctx, snap.ID)
		return nil, err
	}

	snap.Status = "ready"
	if err := s.snapRepo.Update(ctx, snap); err != nil {
		return nil, fmt.Errorf("update snapshot: %w", err)
	}

	return snap, nil
}

func (s *Service) DeleteSnapshot(ctx context.Context, id string) error {
	snap, err := s.snapRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get snapshot: %w", err)
	}
	if snap == nil {
		return fmt.Errorf("snapshot not found")
	}

	vol, err := s.volumeRepo.GetByID(ctx, snap.VolumeID)
	if err != nil {
		return fmt.Errorf("get volume: %w", err)
	}

	pool, err := s.poolRepo.GetByID(ctx, vol.PoolID)
	if err != nil {
		return fmt.Errorf("get pool: %w", err)
	}

	diskPath := filepath.Join(pool.Path, vol.ID+".qcow2")
	if err := qcow2.SnapshotDelete(ctx, diskPath, snap.Name); err != nil {
		return err
	}

	return s.snapRepo.Delete(ctx, id)
}

func (s *Service) RestoreSnapshot(ctx context.Context, snapshotID string) (*domain.Volume, error) {
	snap, err := s.snapRepo.GetByID(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	if snap == nil {
		return nil, fmt.Errorf("snapshot not found")
	}

	vol, err := s.volumeRepo.GetByID(ctx, snap.VolumeID)
	if err != nil {
		return nil, fmt.Errorf("get volume: %w", err)
	}

	pool, err := s.poolRepo.GetByID(ctx, vol.PoolID)
	if err != nil {
		return nil, fmt.Errorf("get pool: %w", err)
	}

	diskPath := filepath.Join(pool.Path, vol.ID+".qcow2")
	if err := qcow2.SnapshotApply(ctx, diskPath, snap.Name); err != nil {
		return nil, err
	}

	return vol, nil
}
