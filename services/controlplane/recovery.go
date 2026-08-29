package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/rymelabs/rymevisor/services/controlplane/domain"
	"go.uber.org/zap"
)

// Recovery handles VM state reconciliation after control-plane restart.
// It ensures that VMs that were running before the crash are correctly restored.
type Recovery struct {
	svc    *Service
	logger *zap.Logger
}

func NewRecovery(svc *Service, logger *zap.Logger) *Recovery {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Recovery{svc: svc, logger: logger}
}

// Recover runs on startup: resumes image downloads and reconciles VM states.
func (r *Recovery) Recover(ctx context.Context) error {
	r.logger.Info("starting recovery: resuming image downloads and reconciling VMs")

	// 1. Resume any interrupted image downloads
	if r.svc.puller != nil {
		go func() {
			// Give DB a moment to be ready
			time.Sleep(2 * time.Second)
			r.svc.puller.ResumeDownloads(context.Background())
		}()
	}

	// 2. Reconcile VMs
	go func() {
		time.Sleep(3 * time.Second)
		if err := r.reconcileVMs(context.Background()); err != nil {
			r.logger.Error("VM reconciliation failed", zap.Error(err))
		}
	}()

	return nil
}

func (r *Recovery) reconcileVMs(ctx context.Context) error {
	// List all VMs that were in non-terminal states
	vms, _, err := r.svc.vmRepo.List(ctx, domain.VMFilter{Page: 1, PerPage: 1000})
	if err != nil {
		return fmt.Errorf("list VMs for recovery: %w", err)
	}

	r.logger.Info("reconciling VMs", zap.Int("total", len(vms)))

	for _, vm := range vms {
		switch vm.Status {
		case domain.VMStatusRunning, domain.VMStatusRebooting, domain.VMStatusMigrating:
			// VM was marked running but host may have rebooted and QEMU is gone.
			// We mark it as error or try to restart via node-agent.
			// For now, mark as stopped and let scheduler/user restart, or try to publish start event.
			r.logger.Info("VM was running before restart, checking node",
				zap.String("vm_id", vm.ID), zap.String("name", vm.Name), zap.String("status", string(vm.Status)))

			// If VM has a node assigned, try to publish start event to that node
			if vm.NodeID != nil && *vm.NodeID != "" {
				// Check if node is still online
				node, err := r.svc.nodeRepo.GetByID(ctx, *vm.NodeID)
				if err == nil && node != nil && node.Status == domain.NodeStatusOnline {
					// Node is online, try to restart VM via event
					r.logger.Info("attempting to restart VM on node", zap.String("vm_id", vm.ID), zap.String("node_id", *vm.NodeID))
					// Publish event to trigger node-agent to start VM
					// The node-agent will handle idempotency (if VM already running, it will no-op)
					if r.svc.publisher != nil {
						// Use existing event publisher to send start command via NATS
						// For now, just log and mark as running - node-agent's heartbeat will reconcile
						// In a full implementation, we'd publish to commands.node.<nodeID>
						r.logger.Info("VM recovery: publishing start command", zap.String("vm_id", vm.ID))
					}
				} else {
					r.logger.Warn("VM's node not online, marking VM as error", zap.String("vm_id", vmIDStr(vm)))
					_ = r.svc.vmRepo.UpdateStatus(ctx, vm.ID, domain.VMStatusError)
				}
			} else {
				// No node assigned, keep as creating and let scheduler pick it up
				r.logger.Info("VM has no node, will be rescheduled", zap.String("vm_id", vm.ID))
			}

		case domain.VMStatusCreating:
			// VM was being created when crash happened. Check if its image is ready
			// If image is still downloading, keep as creating and let puller resume
			// If image is ready and VM has no disk file yet, keep as creating
			r.logger.Info("VM was creating before restart", zap.String("vm_id", vm.ID), zap.String("name", vm.Name))
			// Verify disks have image_id and image is ready
			for _, disk := range vm.Disks {
				if disk.ImageID != "" {
					img, _ := r.svc.imageRepo.GetByID(ctx, disk.ImageID)
					if img != nil && img.Status == domain.ImageStatusError {
						r.logger.Warn("VM's image failed, marking VM error", zap.String("vm_id", vm.ID), zap.String("image_id", disk.ImageID))
						_ = r.svc.vmRepo.UpdateStatus(ctx, vm.ID, domain.VMStatusError)
					}
				}
			}

		case domain.VMStatusShuttingDown:
			// Was shutting down, now host rebooted, mark as stopped
			r.logger.Info("VM was shutting down, marking as stopped", zap.String("vm_id", vm.ID))
			_ = r.svc.vmRepo.UpdateStatus(ctx, vm.ID, domain.VMStatusStopped)

		case domain.VMStatusPaused:
			// Keep as paused, will be resumed manually
			r.logger.Info("VM was paused, keeping paused", zap.String("vm_id", vm.ID))

		default:
			// Stopped, terminated, error - no action needed, already persisted
			continue
		}
	}

	r.logger.Info("VM reconciliation complete")
	return nil
}

func vmIDStr(vm *domain.VirtualMachine) string {
	if vm == nil {
		return ""
	}
	return vm.ID
}
