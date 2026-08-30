package handler

import (
	"context"
	"net/http"

	ws "github.com/rymelabs/rymevisor/internal/ws"
)


var metricsUpgraderCP = ws.Upgrader



func (h *Handler) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	ws.HandleMetrics(w, r, func(ctx context.Context, vmID string) (string, string, int32, int64) {
		vm, _ := h.svc.GetVM(ctx, vmID)
		if vm != nil {
			return vm.Name, string(vm.Status), vm.VCpus, vm.MemoryMB
		}
		return vmID, "unknown", 0, 0
	})
}
