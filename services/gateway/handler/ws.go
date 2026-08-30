package gateway

import (
	"net/http"

	"github.com/gorilla/websocket"
	ws "github.com/rymelabs/rymevisor/internal/ws"
)


var wsUpgrader = ws.Upgrader


func wsAuth(r *http.Request) bool {
	return ws.Auth(r)
}


func logPathsForService(service string) []string {
	return ws.LogPathsForService(service)
}

func streamFile(r *http.Request, conn *websocket.Conn, path, linesParam string) error {
	return ws.StreamFile(r, conn, path, linesParam)
}

func tailFollow(r *http.Request, conn *websocket.Conn, path string) {
	ws.TailFollow(r, conn, path)
}


func (g *Gateway) HandleLogs(w http.ResponseWriter, r *http.Request) {
	ws.HandleLogs(w, r, "all")
}


func (g *Gateway) HandleConsole(w http.ResponseWriter, r *http.Request) {
	ws.HandleConsole(w, r)
}
