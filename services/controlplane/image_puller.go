package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rymelabs/rymevisor/internal/qcow2"

	"github.com/google/uuid"
	"github.com/rymelabs/rymevisor/services/controlplane/catalog"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
	"go.uber.org/zap"
)


type Puller struct {
	imageRepo domain.ImageRepository
	imagesDir string
	logger    *zap.Logger
	mu        sync.Mutex
	active    map[string]bool
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
			Timeout: 0,
		},
	}
}

func (p *Puller) ImagePath(imageID string) string {
	return filepath.Join(p.imagesDir, imageID+".qcow2")
}

func (p *Puller) EnsureDir() error {
	return os.MkdirAll(p.imagesDir, 0o755)
}




func (p *Puller) PullOfficialImage(ctx context.Context, osName, version, arch string) (*domain.Image, error) {
	oi, err := catalog.Find(osName, version, arch)
	if err != nil {
		return nil, err
	}
	return p.PullFromOfficial(ctx, oi)
}

func (p *Puller) PullFromOfficial(ctx context.Context, oi *domain.OfficialImage) (*domain.Image, error) {

	existing, err := p.imageRepo.GetByName(ctx, oi.Name)
	if err != nil {
		return nil, fmt.Errorf("check existing image: %w", err)
	}
	if existing != nil {
		if existing.Status == domain.ImageStatusReady {

			if _, err := os.Stat(p.ImagePath(existing.ID)); err == nil {
				return existing, nil
			}

			p.logger.Warn("image file missing, re-downloading", zap.String("id", existing.ID), zap.String("name", oi.Name))
		}
		if existing.Status == domain.ImageStatusDownloading || existing.Status == domain.ImageStatusProcessing {
			return existing, nil
		}

		_ = p.imageRepo.UpdateStatus(ctx, existing.ID, domain.ImageStatusDownloading, 0, "")
		go p.download(ctx, existing, oi.URL)
		return existing, nil
	}


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


func (p *Puller) ImportFromURL(ctx context.Context, req *domain.ImportImageRequest) (*domain.Image, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

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





func (p *Puller) ResolveAndEnsureImage(ctx context.Context, imageRef string) (*domain.Image, error) {
	if imageRef == "" {
		return nil, fmt.Errorf("image reference is empty")
	}


	if img, err := p.imageRepo.GetByID(ctx, imageRef); err == nil && img != nil {
		if img.Status == domain.ImageStatusError {

			if img.SourceURL != "" {
				_ = p.imageRepo.UpdateStatus(ctx, img.ID, domain.ImageStatusDownloading, 0, "")
				go p.download(context.Background(), img, img.SourceURL)
			}
		}
		return img, nil
	}


	if img, err := p.imageRepo.GetByName(ctx, imageRef); err == nil && img != nil {
		return img, nil
	}


	oi, err := catalog.ResolveImageAlias(imageRef)
	if err != nil {
		return nil, fmt.Errorf("image %q not found and not a known official image: %w", imageRef, err)
	}


	if img, err := p.imageRepo.GetByName(ctx, oi.Name); err == nil && img != nil {
		if img.Status == domain.ImageStatusReady {
			if _, err := os.Stat(p.ImagePath(img.ID)); err == nil {
				return img, nil
			}
		}
		if img.Status == domain.ImageStatusDownloading || img.Status == domain.ImageStatusProcessing {
			return img, nil
		}

		_ = p.imageRepo.UpdateStatus(ctx, img.ID, domain.ImageStatusDownloading, 0, "")
		go p.download(context.Background(), img, oi.URL)
		return img, nil
	}


	img, err := p.PullFromOfficial(ctx, oi)
	if err != nil {
		return nil, err
	}
	return img, nil
}

func (p *Puller) ResumeDownloads(ctx context.Context) {

	images, _, err := p.imageRepo.List(ctx, domain.ImageFilter{})
	if err != nil {
		p.logger.Warn("failed to list images for resume", zap.Error(err))
		return
	}
	for _, img := range images {
		if img.Status == domain.ImageStatusDownloading || img.Status == domain.ImageStatusProcessing {
			if img.SourceURL == "" {
				continue
			}

			if _, err := os.Stat(p.ImagePath(img.ID)); err == nil && img.Status == domain.ImageStatusReady {
				continue
			}
			p.logger.Info("resuming image download on startup", zap.String("id", img.ID), zap.String("name", img.Name), zap.String("status", string(img.Status)))
			go p.download(context.Background(), img, img.SourceURL)
		}
	}
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


	var offset int64
	var hasher = sha256.New()
	if fi, err := os.Stat(tmpPath); err == nil && fi.Size() > 0 {
		offset = fi.Size()
		logger.Info("resuming partial download", zap.Int64("offset", offset))

		if f, err := os.Open(tmpPath); err == nil {
			io.Copy(hasher, f)
			f.Close()
		}
	}


	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		logger.Error("failed to create request", zap.Error(err))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}
	req.Header.Set("User-Agent", "RymeVisor/1.0")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		logger.Error("download failed", zap.Error(err))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}
	defer resp.Body.Close()


	if offset > 0 && resp.StatusCode == http.StatusOK {
		logger.Warn("server does not support resume, restarting download from beginning")
		os.Remove(tmpPath)
		offset = 0
		hasher = sha256.New()
	} else if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		logger.Error("download failed with status", zap.Int("status", resp.StatusCode))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}


	_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusProcessing, 0, "")

	var out *os.File
	if offset > 0 {
		out, err = os.OpenFile(tmpPath, os.O_APPEND|os.O_WRONLY, 0644)
	} else {
		out, err = os.Create(tmpPath)
	}
	if err != nil {
		logger.Error("failed to create tmp file", zap.Error(err))
		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
		return
	}

	writer := io.MultiWriter(out, hasher)

	written, err := io.Copy(writer, resp.Body)
	out.Close()
	if err != nil {
		logger.Error("failed to write image", zap.Error(err))

		_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusDownloading, 0, "")
		return
	}

	totalWritten := offset + written

	checksum := hex.EncodeToString(hasher.Sum(nil))
	logger.Info("download complete", zap.Int64("bytes", totalWritten), zap.String("sha256", checksum))




	ext := filepath.Ext(url)
	isQcow2 := ext == ".qcow2"
	needsConvert := !isQcow2

	var sizeBytes int64 = totalWritten
	var finalChecksum = checksum

	if needsConvert {
		logger.Info("converting image to qcow2", zap.String("tmp", tmpPath), zap.String("final", finalPath))


		sourceFmt := "raw"
		if ext == ".qcow2" {
			sourceFmt = "qcow2"
		}

		os.Remove(finalPath)
		if err := qcow2.Convert(ctx, tmpPath, finalPath, sourceFmt, "qcow2"); err != nil {
			logger.Error("qemu-img convert failed", zap.Error(err))
			_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
			return
		}
		os.Remove(tmpPath)
		if fi, err := os.Stat(finalPath); err == nil {
			sizeBytes = fi.Size()
		}
		if f, err := os.Open(finalPath); err == nil {
			h := sha256.New()
			io.Copy(h, f)
			f.Close()
			finalChecksum = hex.EncodeToString(h.Sum(nil))
		}
	} else {

		os.Remove(finalPath)
		if err := os.Rename(tmpPath, finalPath); err != nil {
			logger.Error("failed to rename", zap.Error(err))
			_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusError, 0, "")
			return
		}
	}


	os.Chmod(finalPath, 0o644)


	_ = p.imageRepo.UpdateStatus(context.Background(), img.ID, domain.ImageStatusReady, sizeBytes, finalChecksum)
	logger.Info("image ready", zap.String("path", finalPath), zap.Int64("size", sizeBytes))


	updatedImg, _ := p.imageRepo.GetByID(context.Background(), img.ID)
	if updatedImg != nil {
		updatedImg.SizeBytes = sizeBytes
		updatedImg.Checksum = finalChecksum
		updatedImg.Status = domain.ImageStatusReady

		_ = p.imageRepo.Update(context.Background(), updatedImg)
	}
}


func (p *Puller) IsReady(imageID string) bool {
	path := p.ImagePath(imageID)
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}


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
