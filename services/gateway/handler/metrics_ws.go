package gateway

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rymelabs/rymevisor/internal/metrics"
)

var metricsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (g *Gateway) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if !wsAuth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}

	// Handle Sec-WebSocket-Protocol
	protocol := r.Header.Get("Sec-WebSocket-Protocol")
	var responseHeader http.Header
	if protocol != "" {
		validKey := os.Getenv("RYMEVISOR_API_KEY")
		if strings.Contains(protocol, validKey) {
			responseHeader = http.Header{}
			responseHeader.Set("Sec-WebSocket-Protocol", validKey)
		}
	}

	conn, err := metricsUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	// Ping handler
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

	vmID := r.URL.Query().Get("vm_id")
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}
	intervalStr := r.URL.Query().Get("interval")
	interval := 1 * time.Second
	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil && d >= 500*time.Millisecond && d <= 10*time.Second {
			interval = d
		}
	}

	collector := metrics.NewCollector()

	// If vm_id provided, stream VM metrics, else system metrics
	if vmID != "" {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				// For gateway, we don't have VM details, so use collector with empty name/status
				// Try to get VM name via proxy? For now, use vmID as name
				vmMetrics, _ := collector.CollectVM(vmID, vmID, "running", 0, 0)
				if vmMetrics == nil {
					vmMetrics = &metrics.VMMetrics{VMID: vmID, Status: "unknown"}
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
		// System metrics
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
