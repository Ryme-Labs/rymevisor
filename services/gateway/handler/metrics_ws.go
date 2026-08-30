package gateway

import (
	"context"
	"net/http"

	ws "github.com/rymelabs/rymevisor/internal/ws"
)

func (g *Gateway) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	ws.HandleMetrics(w, r, func(ctx context.Context, vmID string) (string, string, int32, int64) {
		return vmID, "running", 0, 0
	})
}
