package handler

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

var cpUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool { return true },
}

func cpWsAuth(r *http.Request) bool {
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

func (h *Handler) HandleWsLogs(w http.ResponseWriter, r *http.Request) {
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

	conn, err := cpUpgrader.Upgrade(w, r, responseHeader)
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

	service := r.URL.Query().Get("service")
	if service == "" {
		service = r.URL.Query().Get("vm_id")
		if service != "" {
			service = "vm-" + service
		}
	}
	if service == "" {
		service = "control-plane"
	}

	_ = conn.WriteJSON(map[string]string{"type": "connected", "service": service})

	paths := cpLogPaths(service)
	for _, p := range paths {
		if err := cpStreamFile(r, conn, p, r.URL.Query().Get("lines")); err != nil {
			_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		}
		if service != "all" {
			break
		}
	}

	if r.URL.Query().Get("follow") != "false" && len(paths) > 0 {
		cpTailFollow(r, conn, paths[0])
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}
}

func (h *Handler) HandleWsConsole(w http.ResponseWriter, r *http.Request) {
	if !cpWsAuth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}

	vmID := r.URL.Query().Get("vm_id")
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}
	if vmID == "" {
		vmID = r.URL.Query().Get("service")
	}
	// Also try chi param
	if vmID == "" {
		// fallback try to get from path
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) > 0 {
			candidate := parts[len(parts)-1]
			if candidate != "console" && candidate != "ws" {
				vmID = candidate
			}
		}
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

	conn, err := cpUpgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.WriteJSON(map[string]string{"type": "connected", "vm_id": vmID})

	paths := cpLogPaths("vm-" + vmID)
	for _, p := range paths {
		_ = cpStreamFile(r, conn, p, "50")
	}
	if len(paths) > 0 {
		go cpTailFollow(r, conn, paths[0])
	}
	for {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			break
		}
		_ = conn.WriteJSON(map[string]string{"type": "echo", "line": string(msg)})
	}
}

func cpLogPaths(service string) []string {
	bases := []string{"./.dev-logs", "/var/log/rymevisor", "/tmp", "./"}
	m := map[string][]string{
		"control-plane": {"control-plane.log"},
		"all":           {"control-plane.log", "api-gateway.log", "scheduler.log", "networking-engine.log", "storage-manager.log", "node-agent.log"},
	}
	if service == "all" {
		var files []string
		for _, f := range m["all"] {
			for _, base := range bases {
				p := filepath.Join(base, f)
				if _, err := os.Stat(p); err == nil {
					files = append(files, p)
					break
				}
			}
		}
		if len(files) == 0 {
			files = []string{"/var/log/rymevisor/control-plane.log"}
		}
		return files
	}
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
	names, ok := m[service]
	if !ok {
		names = []string{service + ".log"}
	}
	for _, n := range names {
		for _, base := range bases {
			p := filepath.Join(base, n)
			if _, err := os.Stat(p); err == nil {
				return []string{p}
			}
		}
	}
	return []string{filepath.Join(bases[0], names[0])}
}

func cpStreamFile(r *http.Request, conn *websocket.Conn, path, linesParam string) error {
	f, err := os.Open(path)
	if err != nil {
		_ = conn.WriteJSON(map[string]string{"type": "info", "message": fmt.Sprintf("log file not found: %s", path)})
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

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var all []string
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	start := 0
	if len(all) > lines {
		start = len(all) - lines
	}
	for _, line := range all[start:] {
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := conn.WriteJSON(map[string]string{"type": "log", "service": filepath.Base(path), "line": line}); err != nil {
			return err
		}
	}
	return nil
}

func cpTailFollow(r *http.Request, conn *websocket.Conn, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
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
