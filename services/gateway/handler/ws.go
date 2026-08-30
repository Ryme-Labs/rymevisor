package gateway

import (
	"net/http"

	ws "github.com/rymelabs/rymevisor/internal/ws"
)

func (g *Gateway) HandleLogs(w http.ResponseWriter, r *http.Request) {
	ws.HandleLogs(w, r, "all")
}

func (g *Gateway) HandleConsole(w http.ResponseWriter, r *http.Request) {
	ws.HandleConsole(w, r)
}
