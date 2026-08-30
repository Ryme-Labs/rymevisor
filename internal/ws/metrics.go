package ws

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rymelabs/rymevisor/internal/metrics"
)



type VMResolver func(ctx context.Context, vmID string) (name, status string, vcpus int32, memoryMB int64)

func ExtractMetricsVMID(r *http.Request) string {
	vmID := r.URL.Query().Get("vm_id")
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}
	if vmID == "" {
		vmID = r.PathValue("id")
	}
	return vmID
}


func ParseMetricsInterval(r *http.Request) time.Duration {
	intervalStr := r.URL.Query().Get("interval")
	interval := 1 * time.Second
	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil && d >= 500*time.Millisecond && d <= 10*time.Second {
			interval = d
		}
	}
	return interval
}



func UpgradeMetrics(w http.ResponseWriter, r *http.Request) (*websocket.Conn, bool) {
	if !Auth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return nil, false
	}

	protocol := r.Header.Get("Sec-WebSocket-Protocol")
	var responseHeader http.Header
	if protocol != "" {
		validKey := os.Getenv("RYMEVISOR_API_KEY")
		if validKey != "" && strings.Contains(protocol, validKey) {
			responseHeader = http.Header{}
			responseHeader.Set("Sec-WebSocket-Protocol", validKey)
		}
	}

	conn, err := Upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return nil, false
	}

	setupPingPong(conn)
	return conn, true
}



func HandleMetrics(w http.ResponseWriter, r *http.Request, resolver VMResolver) {
	conn, ok := UpgradeMetrics(w, r)
	if !ok {
		return
	}
	defer conn.Close()

	vmID := ExtractMetricsVMID(r)
	interval := ParseMetricsInterval(r)

	collector := metrics.NewCollector()

	if vmID != "" {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				var name, status string
				var vcpus int32
				var mem int64
				if resolver != nil {
					name, status, vcpus, mem = resolver(r.Context(), vmID)
					if name == "" {
						name = vmID
					}
					if status == "" {
						status = "unknown"
					}
				} else {
					name = vmID
					status = "unknown"
				}
				vmMetrics, _ := collector.CollectVM(vmID, name, status, vcpus, mem)
				if vmMetrics == nil {
					vmMetrics = &metrics.VMMetrics{VMID: vmID, Status: status}
				}
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteJSON(map[string]interface{}{
					"type":      "vm_metrics",
					"timestamp": time.Now().Unix(),
					"vm":        vmMetrics,
				}); err != nil {
					return
				}
			}
		}
	} else {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				sysMetrics, err := collector.CollectSystem()
				if err != nil {
					_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
					continue
				}
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteJSON(map[string]interface{}{
					"type":      "metrics",
					"timestamp": time.Now().Unix(),
					"system":    sysMetrics,
				}); err != nil {
					return
				}
			}
		}
	}
}
