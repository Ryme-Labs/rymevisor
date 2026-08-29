package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

var vmStateUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h *Handler) HandleVMStateWs(w http.ResponseWriter, r *http.Request) {
	if !cpWsAuth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}

	vmID := chi.URLParam(r, "id")
	if vmID == "" {
		vmID = r.URL.Query().Get("vm_id")
	}
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}
	if vmID == "" {
		http.Error(w, `{"error":"vm_id required"}`, http.StatusBadRequest)
		return
	}

	protocol := r.Header.Get("Sec-WebSocket-Protocol")
	var responseHeader http.Header
	if protocol != "" {
		validKey := os.Getenv("RYMEVISOR_API_KEY")
		if strings.Contains(protocol, validKey) {
			responseHeader = http.Header{}
			responseHeader.Set("Sec-WebSocket-Protocol", validKey)
		}
	}

	conn, err := vmStateUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}()

	// Send initial connected
	_ = conn.WriteJSON(map[string]interface{}{
		"type":      "connected",
		"vm_id":     vmID,
		"message":   "streaming VM state logs",
		"timestamp": time.Now().Unix(),
	})

	// Poll VM status and image status, stream changes
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	var lastImageStatus string
	var lastLogOffset int64

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			vm, err := h.svc.GetVM(r.Context(), vmID)
			if err != nil {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "error",
					"message":   fmt.Sprintf("get vm failed: %v", err),
					"timestamp": time.Now().Unix(),
				})
				continue
			}
			if vm == nil {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "error",
					"message":   "vm not found",
					"vm_id":     vmID,
					"timestamp": time.Now().Unix(),
				})
				// Keep polling in case it appears later, but also check dead
				continue
			}

			// Send status if changed or first time
			status := string(vm.Status)
			if status != lastStatus {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "state",
					"vm_id":     vmID,
					"status":    status,
					"prev":      lastStatus,
					"vcpus":     vm.VCpus,
					"memory_mb": vm.MemoryMB,
					"timestamp": time.Now().Unix(),
					"message":   fmt.Sprintf("VM %s -> %s", lastStatus, status),
				})
				lastStatus = status
			} else {
				// Periodic heartbeat with current state
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "state",
					"vm_id":     vmID,
					"status":    status,
					"timestamp": time.Now().Unix(),
				})
			}

			// If VM has image, check image status
			for _, disk := range vm.Disks {
				if disk.ImageID != "" {
					img, err := h.svc.GetImage(r.Context(), disk.ImageID)
					if err == nil && img != nil {
						imgStatus := string(img.Status)
						if imgStatus != lastImageStatus {
							_ = conn.WriteJSON(map[string]interface{}{
								"type":       "image",
								"vm_id":      vmID,
								"image_id":   disk.ImageID,
								"image_name": img.Name,
								"status":     imgStatus,
								"source_url": img.SourceURL,
								"size_bytes": img.SizeBytes,
								"message":    fmt.Sprintf("Image %s: %s", img.Name, imgStatus),
								"timestamp":  time.Now().Unix(),
							})
							lastImageStatus = imgStatus
						}
						// If image is downloading, also stream download progress via file size
						if img.Status == "downloading" || img.Status == "processing" {
							imagePath := filepath.Join("/var/lib/rymevisor/images", disk.ImageID+".qcow2")
							if fi, err := os.Stat(imagePath + ".tmp"); err == nil {
								_ = conn.WriteJSON(map[string]interface{}{
									"type":      "image_progress",
									"vm_id":     vmID,
									"image_id":  disk.ImageID,
									"bytes_downloaded": fi.Size(),
									"timestamp": time.Now().Unix(),
								})
							} else if fi, err := os.Stat(imagePath); err == nil {
								_ = conn.WriteJSON(map[string]interface{}{
									"type":      "image_progress",
									"vm_id":     vmID,
									"image_id":  disk.ImageID,
									"bytes_downloaded": fi.Size(),
									"complete":  true,
									"timestamp": time.Now().Unix(),
								})
							}
						}
					}
					break
				}
			}

			// Stream VM-specific log file tail (if exists)
			logPaths := []string{
				filepath.Join("/var/lib/rymevisor/vms", vmID, "qemu.log"),
				filepath.Join("/var/lib/rymevisor/vms", vmID, "console.log"),
				filepath.Join("./.dev-logs", vmID+".log"),
				filepath.Join("/var/log/rymevisor", vmID+".log"),
			}
			for _, lp := range logPaths {
				if fi, err := os.Stat(lp); err == nil {
					if fi.Size() > lastLogOffset {
						// Read new log lines
						f, err := os.Open(lp)
						if err == nil {
							_, _ = f.Seek(lastLogOffset, 0)
							buf := make([]byte, fi.Size()-lastLogOffset)
							n, _ := f.Read(buf)
							f.Close()
							if n > 0 {
								lines := strings.Split(strings.TrimSpace(string(buf[:n])), "\n")
								for _, line := range lines {
									if line == "" {
										continue
									}
									_ = conn.WriteJSON(map[string]interface{}{
										"type":      "log",
										"vm_id":     vmID,
										"source":    filepath.Base(lp),
										"line":      line,
										"timestamp": time.Now().Unix(),
									})
								}
							}
							lastLogOffset = fi.Size()
						}
					}
					break
				}
			}

			// Also stream control-plane log for this VM (grep vm_id)
			// For now, just send a periodic log count
			_ = conn.WriteJSON(map[string]interface{}{
				"type":      "heartbeat",
				"vm_id":     vmID,
				"status":    status,
				"timestamp": time.Now().Unix(),
			})
		}
	}
}

// Helper to check auth for VM state ws (reuses cpWsAuth logic)
func vmStateAuth(r *http.Request) bool {
	return cpWsAuth(r)
}

// Ensure the handler is wired
func init() {
	// Ensure imports are used
	_ = json.Marshal
	_ = chi.URLParam
}
