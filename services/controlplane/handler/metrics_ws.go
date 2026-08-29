package handler

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rymelabs/rymevisor/internal/metrics"
)

var metricsUpgraderCP = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	if !cpWsAuth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
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

	conn, err := metricsUpgraderCP.Upgrade(w, r, responseHeader)
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

	vmID := r.URL.Query().Get("vm_id")
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}
	// Also try chi param
	if vmID == "" {
		// Try to get from path /metrics/vm/{id}
		if id := r.PathValue("id"); id != "" {
			vmID = id
		}
	}
	intervalStr := r.URL.Query().Get("interval")
	interval := 1 * time.Second
	if intervalStr != "" {
		if d, err := time.ParseDuration(intervalStr); err == nil && d >= 500*time.Millisecond && d <= 10*time.Second {
			interval = d
		}
	}

	collector := metrics.NewCollector()

	if vmID != "" {
		// VM metrics: fetch from DB for details
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				vm, _ := h.svc.GetVM(r.Context(), vmID)
				var name, status string
				var vcpus int32
				var mem int64
				if vm != nil {
					name = vm.Name
					status = string(vm.Status)
					vcpus = vm.VCpus
					mem = vm.MemoryMB
				} else {
					name = vmID
					status = "unknown"
				}
				vmMetrics, _ := collector.CollectVM(vmID, name, status, vcpus, mem)
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


