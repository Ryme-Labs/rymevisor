package handler

import (
	"net/http"

	ws "github.com/rymelabs/rymevisor/internal/ws"
)

func (h *Handler) HandleWsLogs(w http.ResponseWriter, r *http.Request) {
	ws.HandleLogs(w, r, "control-plane")
}

func (h *Handler) HandleWsConsole(w http.ResponseWriter, r *http.Request) {
	ws.HandleConsole(w, r)
}
