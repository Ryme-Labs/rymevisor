package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	ws "github.com/rymelabs/rymevisor/internal/ws"
)

var vmStateUpgraderGW = ws.Upgrader

func (g *Gateway) HandleVMState(w http.ResponseWriter, r *http.Request) {
	if !ws.Auth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}


	vmID := r.URL.Query().Get("vm_id")
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}

	if vmID == "" {

		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		for i, p := range parts {
			if p == "vm" && i+1 < len(parts) {
				vmID = parts[i+1]
				break
			}
		}
	}
	if vmID == "" {
		http.Error(w, `{"error":"vm_id required"}`, http.StatusBadRequest)
		return
	}

	responseHeader := ws.AuthResponseHeader(r)

	conn, err := vmStateUpgraderGW.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	ws.SetupPingPong(conn)

	_ = conn.WriteJSON(map[string]interface{}{
		"type":      "connected",
		"vm_id":     vmID,
		"message":   "streaming VM state (gateway, same API key)",
		"timestamp": time.Now().Unix(),
	})


	controlPlaneURL := g.config.ControlPlaneURL
	if !strings.HasPrefix(controlPlaneURL, "http") {
		controlPlaneURL = "http://" + controlPlaneURL
	}
	apiKey := os.Getenv("RYMEVISOR_API_KEY")
	client := &http.Client{Timeout: 5 * time.Second}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastStatus string
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:

			req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/vms/%s", controlPlaneURL, vmID), nil)
			req.Header.Set("X-API-Key", apiKey)
			resp, err := client.Do(req)
			if err != nil {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "error",
					"message":   fmt.Sprintf("fetch vm failed: %v", err),
					"timestamp": time.Now().Unix(),
				})
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode == 404 {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "error",
					"message":   "vm not found",
					"vm_id":     vmID,
					"timestamp": time.Now().Unix(),
				})
				continue
			}
			if resp.StatusCode != 200 {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "error",
					"message":   fmt.Sprintf("vm fetch status %d", resp.StatusCode),
					"timestamp": time.Now().Unix(),
				})
				continue
			}

			var vm map[string]interface{}
			if err := json.Unmarshal(body, &vm); err != nil {
				continue
			}
			status, _ := vm["status"].(string)
			if status != lastStatus {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "state",
					"vm_id":     vmID,
					"status":    status,
					"prev":      lastStatus,
					"vcpus":     vm["vcpus"],
					"memory_mb": vm["memory_mb"],
					"timestamp": time.Now().Unix(),
					"message":   fmt.Sprintf("VM %s -> %s", lastStatus, status),
				})
				lastStatus = status
			} else {
				_ = conn.WriteJSON(map[string]interface{}{
					"type":      "state",
					"vm_id":     vmID,
					"status":    status,
					"timestamp": time.Now().Unix(),
				})
			}


			if disks, ok := vm["disks"].([]interface{}); ok {
				for _, d := range disks {
					if dm, ok := d.(map[string]interface{}); ok {
						if imageID, ok := dm["image_id"].(string); ok && imageID != "" {

							req2, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/images/%s", controlPlaneURL, imageID), nil)
							req2.Header.Set("X-API-Key", apiKey)
							resp2, err := client.Do(req2)
							if err == nil {
								body2, _ := io.ReadAll(resp2.Body)
								resp2.Body.Close()
								var img map[string]interface{}
								if json.Unmarshal(body2, &img) == nil {
									imgStatus, _ := img["status"].(string)
									_ = conn.WriteJSON(map[string]interface{}{
										"type":       "image",
										"vm_id":      vmID,
										"image_id":   imageID,
										"image_name": img["name"],
										"status":     imgStatus,
										"timestamp":  time.Now().Unix(),
									})
								}
							}
							break
						}
					}
				}
			}

			_ = conn.WriteJSON(map[string]interface{}{
				"type":      "heartbeat",
				"vm_id":     vmID,
				"status":    status,
				"timestamp": time.Now().Unix(),
			})
		}
	}
}
