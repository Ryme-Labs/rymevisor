package qcow2

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
)


type Info struct {
	Format      string `json:"format"`
	VirtualSize int64  `json:"virtual-size"`
	DiskSize    int64  `json:"actual-size"`
	ClusterSize int    `json:"cluster-size"`
	BackingFile string `json:"backing-filename"`
	BackingFmt  string `json:"backing-filename-format"`
}



func formatSize(sizeBytes int64) string {
	if sizeBytes <= 0 {
		return "1G"
	}

	if sizeBytes%(1024*1024*1024) == 0 {
		return fmt.Sprintf("%dG", sizeBytes/(1024*1024*1024))
	}
	return strconv.FormatInt(sizeBytes, 10)
}


func Stat(ctx context.Context, path string) (*Info, error) {
	out, err := exec.CommandContext(ctx, "qemu-img", "info", "--output=json", path).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("qemu-img info %s: %w: %s", path, err, string(out))
	}
	var info Info
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse qemu-img info: %w", err)
	}
	return &info, nil
}


func Create(ctx context.Context, path string, sizeBytes int64) error {
	sizeStr := formatSize(sizeBytes)
	cmd := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", path, sizeStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img create: %w: %s", err, string(out))
	}
	return nil
}

func CreateWithBacking(ctx context.Context, path, backingPath string, sizeBytes int64) error {
	backingFmt := "qcow2"
	if info, err := Stat(ctx, backingPath); err == nil && info.Format != "" {
		backingFmt = info.Format
	} else if filepath.Ext(backingPath) == ".img" {
		backingFmt = "raw"
	}
	cmd := exec.CommandContext(ctx, "qemu-img", "create", "-f", "qcow2", "-b", backingPath, "-F", backingFmt, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img create with backing %s: %w: %s", backingPath, err, string(out))
	}
	if sizeBytes > 0 {

		if err := Resize(ctx, path, sizeBytes); err != nil {

			_ = err
		}
	}
	return nil
}


func Resize(ctx context.Context, path string, sizeBytes int64) error {
	sizeStr := formatSize(sizeBytes)
	cmd := exec.CommandContext(ctx, "qemu-img", "resize", path, sizeStr)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img resize: %w: %s", err, string(out))
	}
	return nil
}


func SnapshotCreate(ctx context.Context, diskPath, snap string) error {
	cmd := exec.CommandContext(ctx, "qemu-img", "snapshot", "-c", snap, diskPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img snapshot create: %w: %s", err, string(out))
	}
	return nil
}


func SnapshotDelete(ctx context.Context, diskPath, snap string) error {
	cmd := exec.CommandContext(ctx, "qemu-img", "snapshot", "-d", snap, diskPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img snapshot delete: %w: %s", err, string(out))
	}
	return nil
}


func SnapshotApply(ctx context.Context, diskPath, snap string) error {
	cmd := exec.CommandContext(ctx, "qemu-img", "snapshot", "-a", snap, diskPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img snapshot apply: %w: %s", err, string(out))
	}
	return nil
}


func Convert(ctx context.Context, src, dst, srcFmt, dstFmt string) error {
	cmd := exec.CommandContext(ctx, "qemu-img", "convert", "-f", srcFmt, "-O", dstFmt, src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("qemu-img convert: %w: %s", err, string(out))
	}
	return nil
}


func Clone(ctx context.Context, srcPath, dstPath string) error {
	return CreateWithBacking(ctx, dstPath, srcPath, 0)
}
