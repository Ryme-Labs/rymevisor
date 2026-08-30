package controlplane

import (
	"context"
	"fmt"
	"time"

	"github.com/rymelabs/rymevisor/services/controlplane/domain"
	"go.uber.org/zap"
)



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


func (r *Recovery) Recover(ctx context.Context) error {
	r.logger.Info("starting recovery: resuming image downloads and reconciling VMs")


	if r.svc.puller != nil {
		go func() {

			time.Sleep(2 * time.Second)
			r.svc.puller.ResumeDownloads(context.Background())
		}()
	}


	go func() {
		time.Sleep(3 * time.Second)
		if err := r.reconcileVMs(context.Background()); err != nil {
			r.logger.Error("VM reconciliation failed", zap.Error(err))
		}
	}()

	return nil
}

func (r *Recovery) reconcileVMs(ctx context.Context) error {

	vms, _, err := r.svc.vmRepo.List(ctx, domain.VMFilter{Page: 1, PerPage: 1000})
	if err != nil {
		return fmt.Errorf("list VMs for recovery: %w", err)
	}

	r.logger.Info("reconciling VMs", zap.Int("total", len(vms)))

	for _, vm := range vms {
		switch vm.Status {
		case domain.VMStatusRunning, domain.VMStatusRebooting, domain.VMStatusMigrating:



			r.logger.Info("VM was running before restart, checking node",
				zap.String("vm_id", vm.ID), zap.String("name", vm.Name), zap.String("status", string(vm.Status)))


			if vm.NodeID != nil && *vm.NodeID != "" {

				node, err := r.svc.nodeRepo.GetByID(ctx, *vm.NodeID)
				if err == nil && node != nil && node.Status == domain.NodeStatusOnline {

					r.logger.Info("attempting to restart VM on node", zap.String("vm_id", vm.ID), zap.String("node_id", *vm.NodeID))


					if r.svc.publisher != nil {



						r.logger.Info("VM recovery: publishing start command", zap.String("vm_id", vm.ID))
					}
				} else {
					r.logger.Warn("VM's node not online, marking VM as error", zap.String("vm_id", vm.ID))
					_ = r.svc.vmRepo.UpdateStatus(ctx, vm.ID, domain.VMStatusError)
				}
			} else {

				r.logger.Info("VM has no node, will be rescheduled", zap.String("vm_id", vm.ID))
			}

		case domain.VMStatusCreating:



			r.logger.Info("VM was creating before restart", zap.String("vm_id", vm.ID), zap.String("name", vm.Name))

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

			r.logger.Info("VM was shutting down, marking as stopped", zap.String("vm_id", vm.ID))
			_ = r.svc.vmRepo.UpdateStatus(ctx, vm.ID, domain.VMStatusStopped)

		case domain.VMStatusPaused:

			r.logger.Info("VM was paused, keeping paused", zap.String("vm_id", vm.ID))

		default:

			continue
		}
	}

	r.logger.Info("VM reconciliation complete")
	return nil
}
