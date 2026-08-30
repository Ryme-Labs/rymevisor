package handler

import (
	"net/http"

	"github.com/gorilla/websocket"
	ws "github.com/rymelabs/rymevisor/internal/ws"
)


var cpUpgrader = ws.Upgrader

func cpWsAuth(r *http.Request) bool {
	return ws.Auth(r)
}

func cpLogPaths(service string) []string {
	return ws.LogPathsForService(service)
}

func cpStreamFile(r *http.Request, conn *websocket.Conn, path, linesParam string) error {
	return ws.StreamFile(r, conn, path, linesParam)
}

func cpTailFollow(r *http.Request, conn *websocket.Conn, path string) {
	ws.TailFollow(r, conn, path)
}


func (h *Handler) HandleWsLogs(w http.ResponseWriter, r *http.Request) {
	ws.HandleLogs(w, r, "control-plane")
}


func (h *Handler) HandleWsConsole(w http.ResponseWriter, r *http.Request) {
	ws.HandleConsole(w, r)
}
