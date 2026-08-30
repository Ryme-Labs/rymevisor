package ws

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


var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{},
}



func Auth(r *http.Request) bool {
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



func LogPathsForService(service string) []string {
	baseCandidates := []string{
		"/var/log/rymevisor",
		"./.dev-logs",
		"/tmp",
		"./",
	}

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
		files = append(files, filepath.Join(baseCandidates[0], n))
	}
	if len(files) == 0 {
		return nil
	}
	return files[:1]
}



func StreamFile(r *http.Request, conn *websocket.Conn, path, linesParam string) error {
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

	scanner := bufio.NewScanner(f)
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


func TailFollow(r *http.Request, conn *websocket.Conn, path string) {
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


func authResponseHeader(r *http.Request) http.Header {
	protocol := r.Header.Get("Sec-WebSocket-Protocol")
	if protocol == "" {
		return nil
	}
	validKey := os.Getenv("RYMEVISOR_API_KEY")
	if validKey != "" && strings.Contains(protocol, validKey) {
		h := http.Header{}
		h.Set("Sec-WebSocket-Protocol", validKey)
		return h
	}
	return nil
}


func setupPingPong(conn *websocket.Conn) {
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
}


func SetupPingPong(conn *websocket.Conn) {
	setupPingPong(conn)
}


func AuthResponseHeader(r *http.Request) http.Header {
	return authResponseHeader(r)
}




func HandleLogs(w http.ResponseWriter, r *http.Request, defaultService string) {
	if !Auth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}

	responseHeader := authResponseHeader(r)

	conn, err := Upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	setupPingPong(conn)

	service := r.URL.Query().Get("service")
	if service == "" {
		service = r.URL.Query().Get("vm_id")
		if service != "" {
			service = "vm-" + service
		}
	}
	if service == "" {
		service = defaultService
		if service == "" {
			service = "all"
		}
	}
	linesParam := r.URL.Query().Get("lines")


	_ = conn.WriteJSON(map[string]string{
		"type":    "connected",
		"service": service,
		"message": fmt.Sprintf("streaming logs for %s", service),
	})

	paths := LogPathsForService(service)

	for _, p := range paths {
		if err := StreamFile(r, conn, p, linesParam); err != nil {
			_ = conn.WriteJSON(map[string]string{"type": "error", "message": err.Error()})
		}
		if service != "all" {
			break
		}
	}

	if r.URL.Query().Get("follow") != "false" {
		if len(paths) > 0 {
			TailFollow(r, conn, paths[0])
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				break
			}
		}
	}
}

func extractVMID(r *http.Request) string {
	vmID := r.URL.Query().Get("vm_id")
	if vmID == "" {
		vmID = r.URL.Query().Get("id")
	}
	if vmID == "" {
		vmID = r.PathValue("id")
	}
	return vmID
}


func HandleConsole(w http.ResponseWriter, r *http.Request) {
	if !Auth(r) {
		http.Error(w, `{"error":"invalid or missing API key"}`, http.StatusUnauthorized)
		return
	}

	vmID := extractVMID(r)
	if vmID == "" {
		http.Error(w, `{"error":"vm_id required"}`, http.StatusBadRequest)
		return
	}

	responseHeader := authResponseHeader(r)

	conn, err := Upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}
	defer conn.Close()

	_ = conn.WriteJSON(map[string]string{"type": "connected", "vm_id": vmID, "message": "console connected (log streaming)"})

	paths := LogPathsForService("vm-" + vmID)
	for _, p := range paths {
		_ = StreamFile(r, conn, p, "50")
	}
	if len(paths) > 0 {
		go TailFollow(r, conn, paths[0])
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
