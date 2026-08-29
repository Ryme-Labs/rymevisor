package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rymelabs/rymevisor/services/controlplane/catalog"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
	"go.uber.org/zap"
)

// Puller handles background downloading and conversion of images.
type Puller struct {
	imageRepo domain.ImageRepository
	imagesDir string
	logger    *zap.Logger
	mu        sync.Mutex
	active    map[string]bool // image ID -> downloading
	client    *http.Client
}

func NewPuller(repo domain.ImageRepository, imagesDir string, logger *zap.Logger) *Puller {
	if logger == nil {
		logger = zap.NewNop()
	}
	if imagesDir == "" {
		imagesDir = "/var/lib/rymevisor/images"
	}
	return &Puller{
		imageRepo: repo,
		imagesDir: imagesDir,
		logger:    logger,
		active:    make(map[string]bool),
		client: &http.Client{
			Timeout: 0, // no timeout for large downloads, context controls
		},
	}
}

func (p *Puller) ImagePath(imageID string) string {
	return filepath.Join(p.imagesDir, imageID+".qcow2")
}

func (p *Puller) RawImagePath(imageID string) string {
	return filepath.Join(p.imagesDir, imageID+".raw")
}

func (p *Puller) EnsureDir() error {
	return os.MkdirAll(p.imagesDir, 0o755)
}

// PullOfficialImage creates a DB entry for the official image and starts background download.
// If an image with same name already exists and is ready, it returns that.
// If downloading, it returns the existing entry.
func (p *Puller) PullOfficialImage(ctx context.Context, osName, version, arch string) (*domain.Image, error) {
	oi, err := catalog.Find(osName, version, arch)
	if err != nil {
		return nil, err
	}
	return p.PullFromOfficial(ctx, oi)
}

func (p *Puller) PullFromOfficial(ctx context.Context, oi *domain.OfficialImage) (*domain.Image, error) {
	// Check if already exists by name
	existing, err := p.imageRepo.GetByName(ctx, oi.Name)
	if err != nil {
		return nil, fmt.Errorf("check existing image: %w", err)
	}
	if existing != nil {
		if existing.Status == domain.ImageStatusReady {
			// Verify file exists
			if _, err := os.Stat(p.ImagePath(existing.ID)); err == nil {
				return existing, nil
			}
			// File missing but DB says ready -> re-download
			p.logger.Warn("image file missing, re-downloading", zap.String("id", existing.ID), zap.String("name", oi.Name))
		}
		if existing.Status == domain.ImageStatusDownloading || existing.Status == domain.ImageStatusProcessing {
			return existing, nil
		}
		// If error status, reset to downloading and retry
		_ = p.imageRepo.UpdateStatus(ctx, existing.ID, domain.ImageStatusDownloading, 0, "")
		go p.download(ctx, existing, oi.URL)
		return existing, nil
	}

	// Create new DB entry
	img := &domain.Image{
		ID:           uuid.New().String(),
		Name:         oi.Name,
		Description:  oi.Description,
		OS:           oi.OS,
		OSVersion:    oi.OSVersion,
		Architecture: oi.Architecture,
		Type:         domain.ImageTypeOS,
		Status:       domain.ImageStatusDownloading,
		SourceURL:    oi.URL,
		SizeBytes:    oi.SizeBytes,
		Tags:         []string{"official", "auto-pulled"},
	}
	if err := p.imageRepo.Create(ctx, img); err != nil {
		return nil, fmt.Errorf("create image: %w", err)
	}

	go p.download(context.Background(), img, oi.URL)

	return img, nil
}

// ImportFromURL creates an image from arbitrary URL and downloads it.
func (p *Puller) ImportFromURL(ctx context.Context, req *domain.ImportImageRequest) (*domain.Image, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	// Check name uniqueness
	existing, err := p.imageRepo.GetByName(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return nil, fmt.Errorf("image with name %q already exists", req.Name)
	}

	img := &domain.Image{
		ID:           uuid.New().String(),
		Name:         req.Name,
		Description:  fmt.Sprintf("Imported from %s", req.URL),
		OS:           req.OS,
		OSVersion:    req.OSVersion,
		Architecture: req.Architecture,
		Type:         domain.ImageTypeOS,
		Status:       domain.ImageStatusDownloading,
		SourceURL:    req.URL,
		Tags:         []string{"imported"},
	}
	if img.Architecture == "" {
		img.Architecture = "amd64"
	}
	if img.OS == "" {
		img.OS = "linux"
	}

	if err := p.imageRepo.Create(ctx, img); err != nil {
		return nil, fmt.Errorf("create image: %w", err)
	}

	go p.download(context.Background(), img, req.URL)

	return img, nil
}

// ResolveAndEnsureImage resolves an image alias or ID and ensures it is pulled.
// imageRef can be: image ID (uuid), name (ubuntu-22.04), or alias (ubuntu, debian).
// If not found in DB, it tries to pull from official catalog.
// Returns the image (may be downloading) or error.
func (p *Puller) ResolveAndEnsureImage(ctx context.Context, imageRef string) (*domain.Image, error) {
	if imageRef == "" {
		return nil, fmt.Errorf("image reference is empty")
	}

	// Try by ID first
	if img, err := p.imageRepo.GetByID(ctx, imageRef); err == nil && img != nil {
		if img.Status == domain.ImageStatusError {
			// Retry download if previously failed
			if img.SourceURL != "" {
				_ = p.imageRepo.UpdateStatus(ctx, img.ID, domain.ImageStatusDownloading, 0, "")
				go p.download(context.Background(), img, img.SourceURL)
			}
		}
		return img, nil
	}

	// Try by name
	if img, err := p.imageRepo.GetByName(ctx, imageRef); err == nil && img != nil {
		return img, nil
	}

	// Try as official alias
	oi, err := catalog.ResolveImageAlias(imageRef)
	if err != nil {
		return nil, fmt.Errorf("image %q not found and not a known official image: %w", imageRef, err)
	}

	// Check DB for official name
	if img, err := p.imageRepo.GetByName(ctx, oi.Name); err == nil && img != nil {
		if img.Status == domain.ImageStatusReady {
			if _, err := os.Stat(p.ImagePath(img.ID)); err == nil {
				return img, nil
			}
		}
		if img.Status == domain.ImageStatusDownloading || img.Status == domain.ImageStatusProcessing {
			return img, nil
		}
		// Re-trigger if error or missing file
		_ = p.imageRepo.UpdateStatus(ctx, img.ID, domain.ImageStatusDownloading, 0, "")
		go p.download(context.Background(), img, oi.URL)
		return img, nil
	}

	// Not in DB, create and pull
	img, err := p.PullFromOfficial(ctx, oi)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (p *Puller) download(ctx context.Context, img *domain.Image, url string) {
	p.mu.Lock()
	if p.active[img.ID] {
		p.mu.Unlock()
		p.logger.Info("download already in progress", zap.String("id", img.ID))
		return
	}
	p.active[img.ID] = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.active, img.ID)
		p.mu.Unlock()
	}()

	logger := p.logger.With(zap.String("image_id", img.ID), zap.String("name", img.Name), zap.String("url", url))
	logger.Info("starting image download")

	if err := p.EnsureDir(); err != nil {
		logger.Error("failed to create images dir", zap.Error(err))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}

	tmpPath := p.ImagePath(img.ID) + ".tmp"
	finalPath := p.ImagePath(img.ID)

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Error("failed to create request", zap.Error(err))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}
	req.Header.Set("User-Agent", "RymeVisor/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		logger.Error("download failed", zap.Error(err))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("download failed with status", zap.Int("status", resp.StatusCode))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}

	// Update status to processing
	_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusProcessing, 0, "")

	out, err := os.Create(tmpPath)
	if err != nil {
		logger.Error("failed to create tmp file", zap.Error(err))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}

	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	written, err := io.Copy(writer, resp.Body)
	out.Close()
	if err != nil {
		logger.Error("failed to write image", zap.Error(err))
		os.Remove(tmpPath)
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	logger.Info("download complete", zap.Int64("bytes", written), zap.String("sha256", checksum))

	// Determine if we need qemu-img convert
	// If file is .img (raw) and we want qcow2, convert
	// Check file extension from URL
	ext := filepath.Ext(url)
	isQcow2 := ext == ".qcow2"
	needsConvert := !isQcow2

	var sizeBytes int64 = written
	var finalChecksum = checksum

	if needsConvert {
		logger.Info("converting image to qcow2", zap.String("tmp", tmpPath), zap.String("final", finalPath))
		// Use qemu-img convert -f raw -O qcow2 tmp final
		// Try to detect format: if url ends with .img assume raw, else try auto
		sourceFmt := "raw"
		if ext == ".qcow2" {
			sourceFmt = "qcow2"
		}
		// Remove final if exists
		os.Remove(finalPath)
		cmd := exec.CommandContext(ctx, "qemu-img", "convert", "-f", sourceFmt, "-O", "qcow2", tmpPath, finalPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Error("qemu-img convert failed, keeping raw", zap.Error(err), zap.String("output", string(output)))
			// Fallback: just move raw to final (keep as is, but rename to .qcow2 still works if raw)
			// Actually keep raw as final .qcow2 is wrong, so just rename tmp to final
			os.Remove(finalPath)
			if err := os.Rename(tmpPath, finalPath); err != nil {
				logger.Error("failed to rename", zap.Error(err))
				_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
				return
			}
		} else {
			// Convert succeeded, remove tmp
			os.Remove(tmpPath)
			// Get actual size of converted file
			if fi, err := os.Stat(finalPath); err == nil {
				sizeBytes = fi.Size()
			}
			// Recalculate checksum of final file
			if f, err := os.Open(finalPath); err == nil {
				h := sha256.New()
				io.Copy(h, f)
				f.Close()
				finalChecksum = hex.EncodeToString(h.Sum(nil))
			}
		}
	} else {
		// Already qcow2, just move
		os.Remove(finalPath)
		if err := os.Rename(tmpPath, finalPath); err != nil {
			logger.Error("failed to rename", zap.Error(err))
			_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
			return
		}
	}

	// Ensure permissions
	os.Chmod(finalPath, 0o644)

	// Update DB to ready
	_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusReady, sizeBytes, finalChecksum)
	logger.Info("image ready", zap.String("path", finalPath), zap.Int64("size", sizeBytes))

	// Also update source_url if needed and refresh updated_at via Update
	updatedImg, _ := p.imageRepo.GetByID(context.Background(), img.ID)
	if updatedImg != nil {
		updatedImg.SizeBytes = sizeBytes
		updatedImg.Checksum = finalChecksum
		updatedImg.Status = domain.ImageStatusReady
		// keep source URL
		_ = p.imageRepo.Update(context.Background(), updatedImg)
	}
}

// IsReady checks if image file exists and is ready.
func (p *Puller) IsReady(imageID string) bool {
	path := p.ImagePath(imageID)
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// WaitReady waits for image to become ready, polling DB and file.
func (p *Puller) WaitReady(ctx context.Context, imageID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		img, err := p.imageRepo.GetByID(ctx, imageID)
		if err != nil {
			return err
		}
		if img != nil && img.Status == domain.ImageStatusReady && p.IsReady(imageID) {
			return nil
		}
		if img != nil && img.Status == domain.ImageStatusError {
			return fmt.Errorf("image %s failed to download", imageID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for image %s to be ready", imageID)
}
