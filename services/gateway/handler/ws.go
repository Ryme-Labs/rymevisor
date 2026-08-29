package gateway

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{},
}

// wsAuth checks API key from header or query (same as RYMEVISOR_API_KEY env)
func wsAuth(r *http.Request) bool {
	validKey := os.Getenv("RYMEVISOR_API_KEY")
	if validKey == "" {
		return false
	}
	provided := r.Header.Get("X-API-Key")
	if provided == "" {
		provided = r.URL.Query().Get("api_key")
	}
	if provided == "" {
		provided = r.URL.Query().Get("token")
	}
	if provided == "" {
		provided = r.URL.Query().Get("key")
	}
	// Also try Sec-WebSocket-Protocol
	if provided == "" {
		if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
			for _, p := range strings.Split(proto, ",") {
				p = strings.TrimSpace(p)
				if p == validKey {
					provided = p
					break
				}
			}
		}
	}
	if provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(validKey)) == 1
}

// HandleLogs upgrades to websocket and streams logs.
// Query params:
//   service: control-plane, api-gateway, scheduler, networking-engine, storage-manager, node-agent, or "all"
//   vm_id: if set, streams VM-specific log (qemu log)
//   lines: number of tail lines to send initially (default 100)
//   follow: if true, tail -f (default true)
func (g *Gateway) HandleLogs(w http.ResponseWriter, r *http.Request) {
	if !wsAuth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}

	// Handle Sec-WebSocket-Protocol echo for auth
	protocol := r.Header.Get("Sec-WebSocket-Protocol")
	var responseHeader http.Header
	if protocol != "" {
		validKey := os.Getenv("RYMEVISOR_API_KEY")
		if strings.Contains(protocol, validKey) {
			responseHeader = http.Header{}
			responseHeader.Set("Sec-WebSocket-Protocol", validKey)
		}
	}

	conn, err := wsUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	// Set ping/pong handlers
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

	service := r.URL.Query().Get("service")
	if service == "" {
		service = r.URL.Query().Get("vm_id")
		if service != "" {
			service = "vm-" + service
		}
	}
	if service == "" {
		service = "all"
	}
	linesParam := r.URL.Query().Get("lines")

	// Send welcome
	_ = conn.WriteJSON(map[string]string{
		"type":    "connected",
		"service": service,
		"message": fmt.Sprintf("streaming logs for %s", service),
	})

	// Determine log sources
	paths := logPathsForService(service)

	// Stream each file
	for _, p := range paths {
		if err := streamFile(r, conn, p, linesParam); err != nil {
			_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		}
		// If single service requested, don't continue to others
		if service != "all" {
			break
		}
	}

	// Keep connection open for follow
	// If service == all, we already streamed all, now tail the most relevant
	if r.URL.Query().Get("follow") != "false" {
		// Tail the first path with follow
		if len(paths) > 0 {
			tailFollow(r, conn, paths[0])
		}
		// Wait for client close
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}
}

func logPathsForService(service string) []string {
	baseCandidates := []string{
		"/var/log/rymevisor",
		"./.dev-logs",
		"/tmp",
		"./",
	}

	// Service name mapping
	serviceMap := map[string][]string{
		"control-plane":     {"control-plane.log"},
		"api-gateway":       {"api-gateway.log"},
		"scheduler":         {"scheduler.log"},
		"networking-engine": {"networking-engine.log", "networking.log"},
		"networking":        {"networking-engine.log"},
		"storage-manager":   {"storage-manager.log", "storage.log"},
		"storage":           {"storage-manager.log"},
		"node-agent":        {"node-agent.log"},
		"all":               {"control-plane.log", "api-gateway.log", "scheduler.log", "networking-engine.log", "storage-manager.log", "node-agent.log"},
	}

	var files []string
	if service == "all" {
		for _, f := range serviceMap["all"] {
			for _, base := range baseCandidates {
				p := filepath.Join(base, f)
				if _, err := os.Stat(p); err == nil {
					files = append(files, p)
					break
				}
			}
		}
		// Also add journal fallback marker
		if len(files) == 0 {
			files = []string{"/var/log/rymevisor/control-plane.log"}
		}
		return files
	}

	// Check if service is vm-*
	if strings.HasPrefix(service, "vm-") {
		vmID := strings.TrimPrefix(service, "vm-")
		vmPaths := []string{
			filepath.Join("/var/lib/rymevisor/vms", vmID, "qemu.log"),
			filepath.Join("/var/lib/rymevisor/vms", vmID, "console.log"),
			filepath.Join("./.dev-logs", vmID+".log"),
		}
		for _, p := range vmPaths {
			if _, err := os.Stat(p); err == nil {
				return []string{p}
			}
		}
		return []string{vmPaths[0]}
	}

	names, ok := serviceMap[service]
	if !ok {
		names = []string{service + ".log"}
	}
	for _, n := range names {
		for _, base := range baseCandidates {
			p := filepath.Join(base, n)
			if _, err := os.Stat(p); err == nil {
				return []string{p}
			}
		}
		// Return first candidate even if not exists, stream will handle missing
		files = append(files, filepath.Join(baseCandidates[0], n))
	}
	if len(files) == 0 {
		files = []string{filepath.Join(baseCandidates[1], service+".log")}
	}
	return files[:1]
}

func streamFile(r *http.Request, conn *websocket.Conn, path, linesParam string) error {
	f, err := os.Open(path)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "info", "message": fmt.Sprintf("log file not found: %s (service may not have started yet)", path)})
		return nil
	}
	defer f.Close()

	lines := 100
	if linesParam != "" {
		fmt.Sscanf(linesParam, "%d", &lines)
		if lines < 0 {
			lines = 0
		}
		if lines > 1000 {
			lines = 1000
		}
	}

	// Read last N lines
	scanner := bufio.NewScanner(f)
	// Allow large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var allLines []string
	for scanner.Scan() {
		allLines = append(allLines, scanner.Text())
	}
	start := 0
	if len(allLines) > lines {
		start = len(allLines) - lines
	}
	for _, line := range allLines[start:] {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(map[string]string{"type": "log", "service": filepath.Base(path), "line": line}); err != nil {
			return err
		}
	}
	return nil
}

func tailFollow(r *http.Request, conn *websocket.Conn, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	// Seek to end
	_, _ = f.Seek(0, 2)
	reader := bufio.NewReader(f)

	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			// No new data, wait
			time.Sleep(500 * time.Millisecond)
			continue
		}
		line = strings.TrimRight(line, "\r\n")
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(map[string]string{"type": "log", "service": filepath.Base(path), "line": line}); err != nil {
			return
		}
	}
}

// HandleConsole proxies VM console (QMP/monitor) over websocket.
// For now, it streams VM log and accepts input to forward to monitor socket if available.
// Query: vm_id required.
func (g *Gateway) HandleConsole(w http.ResponseWriter, r *http.Request) {
	if !wsAuth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}

	vmID := r.URL.Query().Get("vm_id")
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}
	if vmID == "" {
		// Try chi URL param if routed as /ws/console/{id}
		vmID = r.PathValue("id")
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

	conn, err := wsUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.WriteJSON(map[string]string{"type": "connected", "vm_id": vmID, "message": "console connected (log streaming)"})

	// Stream VM log
	paths := logPathsForService("vm-" + vmID)
	for _, p := range paths {
		_ = streamFile(r, conn, p, "50")
	}
	// Tail
	if len(paths) > 0 {
		go tailFollow(r, conn, paths[0])
	}

	// Echo any client messages
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		// For now, just echo back
		_ = conn.WriteJSON(map[string]string{"type": "echo", "line": string(msg)})
	}
}
