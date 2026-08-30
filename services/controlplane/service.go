package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
	"github.com/rymelabs/rymevisor/internal/ipam"
	"github.com/rymelabs/rymevisor/internal/mac"
	inats "github.com/rymelabs/rymevisor/internal/nats"
	"github.com/rymelabs/rymevisor/services/controlplane/catalog"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
	"go.uber.org/zap"
)

type IPAMAllocator interface {
	EnsureDefaultNetwork(ctx context.Context, organizationID string) (string, string, error)
	AllocateIP(ctx context.Context, networkID string) (string, string, string, error)
}

type Service struct {
	vmRepo     domain.VMRepository
	nodeRepo   domain.NodeRepository
	imageRepo  domain.ImageRepository
	backupRepo domain.BackupRepository
	snapRepo   domain.SnapshotRepository
	flavorRepo domain.FlavorRepository
	keypairRepo domain.KeypairRepository
	ipam       IPAMAllocator
	publisher  EventPublisher
	puller     *Puller
	logger     *zap.Logger
}

type EventPublisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
	PublishVMEvent(ctx context.Context, eventType, vmID string, data []byte) error
	PublishNodeEvent(ctx context.Context, eventType, nodeID string, data []byte) error
}

func NewService(
	vmRepo domain.VMRepository,
	nodeRepo domain.NodeRepository,
	imageRepo domain.ImageRepository,
	backupRepo domain.BackupRepository,
	snapRepo domain.SnapshotRepository,
	publisher EventPublisher,
) *Service {
	return &Service{
		vmRepo:     vmRepo,
		nodeRepo:   nodeRepo,
		imageRepo:  imageRepo,
		backupRepo: backupRepo,
		snapRepo:   snapRepo,
		publisher:  publisher,
		logger:     zap.NewNop(),
	}
}

func (s *Service) SetLogger(logger *zap.Logger) {
	if logger != nil {
		s.logger = logger
		if s.puller != nil {
			s.puller.logger = logger
		}
	}
}

func (s *Service) InitPuller(imagesDir string, logger *zap.Logger) {
	if logger == nil {
		logger = s.logger
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	s.puller = NewPuller(s.imageRepo, imagesDir, logger)
}

func (s *Service) SetFlavorRepository(repo domain.FlavorRepository) {
	s.flavorRepo = repo
}

func (s *Service) SetKeypairRepository(repo domain.KeypairRepository) {
	s.keypairRepo = repo
}

func (s *Service) SetIPAMAllocator(ipam IPAMAllocator) {
	s.ipam = ipam
}

func (s *Service) CreateVM(ctx context.Context, req *domain.CreateVMRequest) (*domain.VirtualMachine, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("vm name is required")
	}


	if req.FlavorID != "" || req.Flavor != "" {
		if s.flavorRepo != nil {
			var flavor *domain.Flavor
			var err error
			if req.FlavorID != "" {
				flavor, err = s.flavorRepo.GetByID(ctx, req.FlavorID)
			} else {
				flavor, err = s.flavorRepo.GetByName(ctx, req.Flavor)
			}
			if err != nil {
				return nil, fmt.Errorf("get flavor: %w", err)
			}
			if flavor == nil {
				return nil, fmt.Errorf("flavor %q not found", req.Flavor+req.FlavorID)
			}

			if req.VCpus == 0 {
				req.VCpus = flavor.VCpus
			}
			if req.MemoryMB == 0 {
				req.MemoryMB = flavor.MemoryMB
			}

			if len(req.Disks) == 0 && flavor.DiskGB > 0 {
				req.Disks = []domain.CreateDiskRequest{
					{Name: "root", SizeBytes: flavor.DiskGB * 1024 * 1024 * 1024, Type: "qcow2", StoragePool: "default"},
				}
			}
		} else if req.VCpus == 0 || req.MemoryMB == 0 {
			return nil, fmt.Errorf("flavor not available, specify vcpus and memory directly")
		}
	}


	if req.Keypair != "" && req.KeypairID == "" && s.keypairRepo != nil {
		if kp, _ := s.keypairRepo.GetByName(ctx, req.Keypair, ""); kp != nil {
			req.KeypairID = kp.ID
		}
	}

	if req.VCpus < 1 {
		return nil, fmt.Errorf("vcpus must be at least 1")
	}
	if req.MemoryMB < 256 {
		return nil, fmt.Errorf("memory must be at least 256 MB")
	}




	orgID := "00000000-0000-0000-0000-000000000000"
	if req.Labels != nil && req.Labels["organization_id"] != "" {
		orgID = req.Labels["organization_id"]
	} else if req.Metadata != nil && req.Metadata["organization_id"] != "" {
		orgID = req.Metadata["organization_id"]
	}

	vm := &domain.VirtualMachine{
		ID:             uuid.New().String(),
		Name:           req.Name,
		OrganizationID: orgID,
		Status:         domain.VMStatusCreating,
		VCpus:          req.VCpus,
		MemoryMB:       req.MemoryMB,
		CPUModel:       req.CPUModel,
		MachineType:    req.MachineType,
		EnableSecureBoot: req.EnableSecureBoot,
		EnableTPM:      req.EnableTPM,
		Hugepages:      req.Hugepages,
		CloudInit:      req.CloudInit,
		Tags:           req.Tags,
		Metadata:       req.Metadata,
		Labels:         req.Labels,
	}

	if req.KeypairID != "" {
		vm.SSHKeyID = &req.KeypairID
	}

	if req.NodeID != "" {
		vm.NodeID = &req.NodeID
	}

	if len(req.Disks) == 0 {
		vm.Disks = []domain.Disk{
			{
				ID:          uuid.New().String(),
				Name:        "root",
				SizeBytes:   20 * 1024 * 1024 * 1024,
				Type:        "qcow2",
				StoragePool: "default",
				Boot:        true,
				Order:       0,
			},
		}
	} else {
		for i := range req.Disks {
			diskReq := req.Disks[i]
			imageID := diskReq.ImageID


			if imageID == "" && diskReq.Image != "" {
				if s.puller != nil {
					if img, err := s.puller.ResolveAndEnsureImage(ctx, diskReq.Image); err == nil && img != nil {
						imageID = img.ID

						if diskReq.SizeBytes == 0 && img.SizeBytes > 0 {
							diskReq.SizeBytes = img.SizeBytes
						}
					} else if err != nil {
						if oi, cerr := catalog.ResolveImageAlias(diskReq.Image); cerr == nil {
							if pulled, perr := s.PullOfficialImage(ctx, oi.OS, oi.OSVersion, oi.Architecture); perr == nil && pulled != nil {
								imageID = pulled.ID
							}
						}
						if imageID == "" {
							return nil, fmt.Errorf("image %q not found: %w", diskReq.Image, err)
						}
					}
				} else {

					if img, _ := s.imageRepo.GetByName(ctx, diskReq.Image); img != nil {
						imageID = img.ID
					} else if oi, err := catalog.ResolveImageAlias(diskReq.Image); err == nil {


						return nil, fmt.Errorf("image %q not cached, pull it first via POST /api/v1/images/pull {\"os\":\"%s\",\"os_version\":\"%s\",\"architecture\":\"%s\"}", diskReq.Image, oi.OS, oi.OSVersion, oi.Architecture)
					} else {
						return nil, fmt.Errorf("unknown image %q", diskReq.Image)
					}
				}
			} else if imageID != "" {

				if s.puller != nil {
					if img, err := s.puller.ResolveAndEnsureImage(ctx, imageID); err != nil {
						return nil, fmt.Errorf("image %q: %w", imageID, err)
					} else if img != nil {
						imageID = img.ID
					}
				} else {
					if img, _ := s.imageRepo.GetByID(ctx, imageID); img == nil {
						return nil, fmt.Errorf("image %q not found", imageID)
					}
				}
			}

			sizeBytes := diskReq.SizeBytes
			if sizeBytes == 0 {
				sizeBytes = 20 * 1024 * 1024 * 1024
			}
			diskType := diskReq.Type
			if diskType == "" {
				diskType = "qcow2"
			}
			pool := diskReq.StoragePool
			if pool == "" {
				pool = "default"
			}
			name := diskReq.Name
			if name == "" {
				name = fmt.Sprintf("disk-%d", i)
				if i == 0 {
					name = "root"
				}
			}

			vm.Disks = append(vm.Disks, domain.Disk{
				ID:          uuid.New().String(),
				Name:        name,
				SizeBytes:   sizeBytes,
				Type:        diskType,
				StoragePool: pool,
				ImageID:     imageID,
				Boot:        i == 0,
				Order:       int32(i),
			})
		}
	}




	if len(req.NetworkInterfaces) > 0 {
		for i, nicReq := range req.NetworkInterfaces {
			networkID := nicReq.NetworkID
			if networkID == "" && s.ipam != nil {

				defID, _, err := s.ipam.EnsureDefaultNetwork(ctx, orgID)
				if err != nil {
					return nil, fmt.Errorf("ensure default network: %w", err)
				}
				networkID = defID
			}
			nicID := uuid.New().String()
			macAddr := mac.GenerateNICMAC(vm.ID, nicID)
			var ipv4 []string
			if s.ipam != nil && networkID != "" {
				allocated, _, _, err := s.ipam.AllocateIP(ctx, networkID)
				if err != nil {
					return nil, fmt.Errorf("allocate IP in network %s: %w", networkID, err)
				}
				ipv4 = []string{allocated}
			}
			isPrimary := nicReq.IsPrimary
			if i == 0 && !hasPrimary(req.NetworkInterfaces) {
				isPrimary = true
			}
			vm.NetworkInterfaces = append(vm.NetworkInterfaces, domain.NetworkInterface{
				ID:            nicID,
				Name:          fmt.Sprintf("eth%d", i),
				NetworkID:     networkID,
				MACAddress:    macAddr,
				IPv4Addresses: ipv4,
				IsPrimary:     isPrimary,
			})
		}
	} else {
		networkID := ""
		if s.ipam != nil {
			defID, _, err := s.ipam.EnsureDefaultNetwork(ctx, orgID)
			if err != nil {
				return nil, fmt.Errorf("ensure default network: %w", err)
			}
			networkID = defID
		}
		nicID := uuid.New().String()
		macAddr := mac.GenerateNICMAC(vm.ID, nicID)
		var ipv4 []string
		if s.ipam != nil && networkID != "" {
			allocated, _, _, err := s.ipam.AllocateIP(ctx, networkID)
			if err != nil {
				return nil, fmt.Errorf("allocate IP in default network: %w", err)
			}
			ipv4 = []string{allocated}
		}
		vm.NetworkInterfaces = []domain.NetworkInterface{
			{
				ID:            nicID,
				Name:          "eth0",
				NetworkID:     networkID,
				MACAddress:    macAddr,
				IPv4Addresses: ipv4,
				IsPrimary:     true,
			},
		}
	}


	for attempt := 0; attempt < 3; attempt++ {
		err := s.vmRepo.Create(ctx, vm)
		if err == nil {
			break
		}
		if isUniqueViolation(err) {
			s.logger.Warn("IP/MAC collision on concurrent create, retrying", zap.Error(err), zap.String("vm_id", vm.ID))

			for idx := range vm.NetworkInterfaces {
				nic := &vm.NetworkInterfaces[idx]
				if s.ipam != nil && nic.NetworkID != "" {
					allocated, _, _, aerr := s.ipam.AllocateIP(ctx, nic.NetworkID)
					if aerr == nil {
						nic.IPv4Addresses = []string{allocated}
					}
					nic.MACAddress = mac.GenerateNICMAC(vm.ID, nic.ID+fmt.Sprintf("-%d", attempt))
				}
			}
			if attempt == 2 {
				return nil, fmt.Errorf("create vm: %w", err)
			}
			continue
		}
		return nil, fmt.Errorf("create vm: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{
		"vm_id":  vm.ID,
		"status": string(vm.Status),
	})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "created", vm.ID, eventData)
	}

	return vm, nil
}

func (s *Service) GetVM(ctx context.Context, id string) (*domain.VirtualMachine, error) {
	return s.vmRepo.GetByID(ctx, id)
}

func (s *Service) ListVMs(ctx context.Context, filter domain.VMFilter) ([]*domain.VirtualMachine, int, error) {
	return s.vmRepo.List(ctx, filter)
}

func (s *Service) UpdateVM(ctx context.Context, id string, req *domain.UpdateVMRequest) (*domain.VirtualMachine, error) {
	vm, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm not found")
	}

	if req.Name != "" {
		vm.Name = req.Name
	}
	if req.Metadata != nil {
		vm.Metadata = req.Metadata
	}
	if req.Labels != nil {
		vm.Labels = req.Labels
	}
	if req.Tags != nil {
		vm.Tags = req.Tags
	}

	if err := s.vmRepo.Update(ctx, vm); err != nil {
		return nil, fmt.Errorf("update vm: %w", err)
	}

	return vm, nil
}

func (s *Service) DeleteVM(ctx context.Context, id string, force bool) error {
	vm, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return fmt.Errorf("vm not found")
	}

	if !force && vm.Status != domain.VMStatusStopped {
		return fmt.Errorf("vm must be stopped before deletion (current status: %s)", vm.Status)
	}

	if err := s.vmRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete vm: %w", err)
	}

	return nil
}

func (s *Service) PowerOn(ctx context.Context, id string) (*domain.VirtualMachine, error) {
	vm, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm not found")
	}

	if vm.Status != domain.VMStatusStopped && vm.Status != domain.VMStatusPaused && vm.Status != domain.VMStatusCreating {
		return nil, fmt.Errorf("vm must be stopped or paused to power on (current status: %s)", vm.Status)
	}


	if vm.NodeID == nil || *vm.NodeID == "" {
		nodes, _, err := s.nodeRepo.List(ctx, domain.NodeFilter{Status: "online", Page: 1, PerPage: 1})
		if err == nil && len(nodes) > 0 {
			nodeID := nodes[0].ID
			_ = s.vmRepo.AssignNode(ctx, id, nodeID)
			vm.NodeID = &nodeID
			s.logger.Info("auto-scheduled VM to node", zap.String("vm_id", id), zap.String("node_id", nodeID))
		} else {

			nodes, _, _ = s.nodeRepo.List(ctx, domain.NodeFilter{Page: 1, PerPage: 1})
			if len(nodes) > 0 {
				nodeID := nodes[0].ID
				_ = s.vmRepo.AssignNode(ctx, id, nodeID)
				vm.NodeID = &nodeID
			}
		}
	}

	if err := s.vmRepo.UpdateStatus(ctx, id, domain.VMStatusRunning); err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}
	vm.Status = domain.VMStatusRunning


	if s.publisher != nil && vm.NodeID != nil && *vm.NodeID != "" {
		if err := s.publishVMStart(ctx, vm); err != nil {
			s.logger.Warn("publish VM start command failed", zap.Error(err), zap.String("vm_id", id))
		}
	}

	eventData, _ := json.Marshal(map[string]string{"vm_id": id})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "started", id, eventData)
	}
	return vm, nil
}

func (s *Service) publishVMStart(ctx context.Context, vm *domain.VirtualMachine) error {
	if s.publisher == nil {
		return nil
	}

	type nicConfig struct {
		NetworkID  string `json:"network_id"`
		MACAddress string `json:"mac_address"`
		IPAddress  string `json:"ip_address"`
		Gateway    string `json:"gateway,omitempty"`
		Bridge     string `json:"bridge,omitempty"`
		Name       string `json:"name,omitempty"`
	}
	type vmStartCfg struct {
		Name              string      `json:"name"`
		VCPUs             int32       `json:"vcpus"`
		MemoryMB          int64       `json:"memory_mb"`
		MACAddress        string      `json:"mac_address,omitempty"`
		SSHKey            string      `json:"ssh_key,omitempty"`
		ImageID           string      `json:"image_id,omitempty"`
		SizeBytes         int64       `json:"size_bytes,omitempty"`
		MachineType       string      `json:"machine_type,omitempty"`
		NetworkInterfaces []nicConfig `json:"network_interfaces,omitempty"`
	}
	type vmCommand struct {
		Action string      `json:"action"`
		VMID   string      `json:"vm_id"`
		Config *vmStartCfg `json:"config,omitempty"`
	}

	var nics []nicConfig
	legacyMAC := ""
	for i, nic := range vm.NetworkInterfaces {
		macAddr := nic.MACAddress
		if macAddr == "" {
			macAddr = mac.GenerateNICMAC(vm.ID, nic.ID)
		}
		if i == 0 {
			legacyMAC = macAddr
		}
		ip := ""
		if len(nic.IPv4Addresses) > 0 {
			ip = nic.IPv4Addresses[0]
		}
		gw := ""
		if ip != "" {
			if g, err := gatewayFromIP(ip); err == nil {
				gw = g
			}
		}
		bridge, err := ipam.BridgeForNetwork(nic.NetworkID)
		if err != nil {
			return fmt.Errorf("bridge for network %s: %w", nic.NetworkID, err)
		}
		nics = append(nics, nicConfig{
			NetworkID:  nic.NetworkID,
			MACAddress: macAddr,
			IPAddress:  ip,
			Gateway:    gw,
			Bridge:     bridge,
			Name:       nic.Name,
		})
	}
	if len(nics) == 0 {
		return fmt.Errorf("no NICs provided for VM %s", vm.ID)
	}

	sshKey := ""
	if vm.SSHKeyID != nil && *vm.SSHKeyID != "" && s.keypairRepo != nil {
		if kp, _ := s.keypairRepo.GetByID(ctx, *vm.SSHKeyID); kp != nil {
			sshKey = kp.PublicKey
		}
	}
	imageID := ""
	sizeBytes := int64(0)
	if len(vm.Disks) > 0 {
		for _, d := range vm.Disks {
			if d.Boot {
				imageID = d.ImageID
				sizeBytes = d.SizeBytes
				break
			}
		}
		if imageID == "" {
			imageID = vm.Disks[0].ImageID
			sizeBytes = vm.Disks[0].SizeBytes
		}
	}

	cfg := &vmStartCfg{
		Name:              vm.Name,
		VCPUs:             vm.VCpus,
		MemoryMB:          vm.MemoryMB,
		MACAddress:        legacyMAC,
		SSHKey:            sshKey,
		ImageID:           imageID,
		SizeBytes:         sizeBytes,
		MachineType:       vm.MachineType,
		NetworkInterfaces: nics,
	}
	cmd := vmCommand{Action: "start", VMID: vm.ID, Config: cfg}
	data, _ := json.Marshal(cmd)
	subject := inats.SubjectForNode(*vm.NodeID, "start")
	return s.publisher.Publish(ctx, subject, data)
}

func gatewayFromIP(cidrOrIP string) (string, error) {
	_, ipNet, err := parseCIDR(cidrOrIP)
	if err != nil {
		return "", err
	}
	gwIP := make([]byte, len(ipNet.IP))
	copy(gwIP, ipNet.IP)
	if len(gwIP) == 16 {
		if v4 := net.ParseIP(ipNet.IP.String()).To4(); v4 != nil {
			gwIP = v4
		}
	}
	for i := len(gwIP) - 1; i >= 0; i-- {
		gwIP[i]++
		if gwIP[i] != 0 {
			break
		}
	}
	return net.IP(gwIP).String(), nil
}

func parseCIDR(cidr string) (net.IP, *net.IPNet, error) {
	if ip, ipNet, err := net.ParseCIDR(cidr); err == nil {
		return ip, ipNet, nil
	}
	if ip := net.ParseIP(cidr); ip != nil {

		_, ipNet, _ := net.ParseCIDR(ip.String() + "/24")
		return ip, ipNet, nil
	}
	return nil, nil, fmt.Errorf("invalid CIDR %q", cidr)
}

func (s *Service) PowerOff(ctx context.Context, id string, force bool) (*domain.VirtualMachine, error) {
	vm, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm not found")
	}

	if vm.Status != domain.VMStatusRunning && vm.Status != domain.VMStatusPaused && !force {
		return nil, fmt.Errorf("vm must be running or paused to power off (current status: %s)", vm.Status)
	}

	if err := s.vmRepo.UpdateStatus(ctx, id, domain.VMStatusShuttingDown); err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}


	if s.publisher != nil && vm.NodeID != nil && *vm.NodeID != "" {
		type cmd struct {
			Action string `json:"action"`
			VMID   string `json:"vm_id"`
			Force  bool   `json:"force,omitempty"`
		}
		data, _ := json.Marshal(cmd{Action: "stop", VMID: id, Force: force})
		_ = s.publisher.Publish(ctx, inats.SubjectForNode(*vm.NodeID, "stop"), data)
	}

	if err := s.vmRepo.UpdateStatus(ctx, id, domain.VMStatusStopped); err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}

	vm.Status = domain.VMStatusStopped

	eventData, _ := json.Marshal(map[string]string{"vm_id": id})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "stopped", id, eventData)
	}

	return vm, nil
}

func (s *Service) Reboot(ctx context.Context, id string, force bool) (*domain.VirtualMachine, error) {
	vm, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm not found")
	}

	if vm.Status != domain.VMStatusRunning && !force {
		return nil, fmt.Errorf("vm must be running to reboot (current status: %s)", vm.Status)
	}

	if err := s.vmRepo.UpdateStatus(ctx, id, domain.VMStatusRebooting); err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}

	if s.publisher != nil && vm.NodeID != nil && *vm.NodeID != "" {
		type cmd struct {
			Action string `json:"action"`
			VMID   string `json:"vm_id"`
			Force  bool   `json:"force,omitempty"`
		}
		data, _ := json.Marshal(cmd{Action: "reboot", VMID: id, Force: force})
		_ = s.publisher.Publish(ctx, inats.SubjectForNode(*vm.NodeID, "reboot"), data)
	}

	if err := s.vmRepo.UpdateStatus(ctx, id, domain.VMStatusRunning); err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}

	vm.Status = domain.VMStatusRunning

	eventData, _ := json.Marshal(map[string]string{"vm_id": id})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "rebooted", id, eventData)
	}

	return vm, nil
}

func (s *Service) Resize(ctx context.Context, id string, vcpus int32, memoryMB int64) (*domain.VirtualMachine, error) {
	vm, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm not found")
	}

	if vcpus < 1 {
		return nil, fmt.Errorf("vcpus must be at least 1")
	}
	if memoryMB < 256 {
		return nil, fmt.Errorf("memory must be at least 256 MB")
	}

	vm.VCpus = vcpus
	vm.MemoryMB = memoryMB

	if err := s.vmRepo.Update(ctx, vm); err != nil {
		return nil, fmt.Errorf("update vm: %w", err)
	}

	eventData, _ := json.Marshal(map[string]any{
		"vm_id":     id,
		"vcpus":     vcpus,
		"memory_mb": memoryMB,
	})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "resized", id, eventData)
	}

	return vm, nil
}

func (s *Service) Snapshot(ctx context.Context, vmID, name, description string) (*domain.Snapshot, error) {
	vm, err := s.vmRepo.GetByID(ctx, vmID)
	if err != nil {
		return nil, fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm not found")
	}

	snap := &domain.Snapshot{
		ID:          uuid.New().String(),
		VMID:        vmID,
		Name:        name,
		Description: description,
		Status:      "creating",
	}

	if err := s.snapRepo.Create(ctx, snap); err != nil {
		return nil, fmt.Errorf("create snapshot: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{
		"vm_id":       vmID,
		"snapshot_id": snap.ID,
	})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "snapshot_created", vmID, eventData)
	}

	return snap, nil
}

func (s *Service) Clone(ctx context.Context, id string, name string, nodeID string, linked bool) (*domain.VirtualMachine, error) {
	source, err := s.vmRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get source vm: %w", err)
	}
	if source == nil {
		return nil, fmt.Errorf("source vm not found")
	}

	cloned := &domain.VirtualMachine{
		ID:                 uuid.New().String(),
		Name:               name,
		OrganizationID:     source.OrganizationID,
		Status:             domain.VMStatusCreating,
		VCpus:              source.VCpus,
		MemoryMB:           source.MemoryMB,
		CPUModel:           source.CPUModel,
		MachineType:        source.MachineType,
		EnableSecureBoot:   source.EnableSecureBoot,
		EnableTPM:          source.EnableTPM,
		Hugepages:          source.Hugepages,
		CloudInit:          source.CloudInit,
		Tags:               make([]string, len(source.Tags)),
		Metadata:           make(map[string]string),
		Labels:             make(map[string]string),
		NetworkInterfaces:  make([]domain.NetworkInterface, 0),
	}

	copy(cloned.Tags, source.Tags)
	for k, v := range source.Metadata {
		cloned.Metadata[k] = v
	}
	for k, v := range source.Labels {
		cloned.Labels[k] = v
	}

	if nodeID != "" {
		cloned.NodeID = &nodeID
	} else if source.NodeID != nil {
		cloned.NodeID = source.NodeID
	}

	cloned.Disks = make([]domain.Disk, len(source.Disks))
	for i, d := range source.Disks {
		cloned.Disks[i] = domain.Disk{
			ID:          uuid.New().String(),
			Name:        d.Name,
			SizeBytes:   d.SizeBytes,
			Type:        d.Type,
			StoragePool: d.StoragePool,
			Boot:        d.Boot,
			Order:       d.Order,
		}
	}

	for i, nic := range source.NetworkInterfaces {
		nicID := uuid.New().String()
		macAddr := mac.GenerateNICMAC(cloned.ID, nicID)
		var ipv4 []string
		if s.ipam != nil && nic.NetworkID != "" {
			if allocated, _, _, err := s.ipam.AllocateIP(ctx, nic.NetworkID); err == nil {
				ipv4 = []string{allocated}
			}
		}
		if macAddr == "" {
			macAddr = mac.GenerateNICMAC(cloned.ID, fmt.Sprintf("%d", i))
		}
		cloned.NetworkInterfaces = append(cloned.NetworkInterfaces, domain.NetworkInterface{
			ID:            nicID,
			Name:          fmt.Sprintf("eth%d", i),
			NetworkID:     nic.NetworkID,
			MACAddress:    macAddr,
			IPv4Addresses: ipv4,
			IsPrimary:     nic.IsPrimary,
		})
	}

	if len(cloned.NetworkInterfaces) == 0 && s.ipam != nil {
		defID, _, _ := s.ipam.EnsureDefaultNetwork(ctx, cloned.OrganizationID)
		nicID := uuid.New().String()
		allocated, _, _, _ := s.ipam.AllocateIP(ctx, defID)
		cloned.NetworkInterfaces = []domain.NetworkInterface{{
			ID:            nicID,
			Name:          "eth0",
			NetworkID:     defID,
			MACAddress:    mac.GenerateNICMAC(cloned.ID, nicID),
			IPv4Addresses: []string{allocated},
			IsPrimary:     true,
		}}
	}

	if err := s.vmRepo.Create(ctx, cloned); err != nil {

		if isUniqueViolation(err) {
			for idx := range cloned.NetworkInterfaces {
				n := &cloned.NetworkInterfaces[idx]
				if s.ipam != nil && n.NetworkID != "" {
					if a, _, _, e := s.ipam.AllocateIP(ctx, n.NetworkID); e == nil {
						n.IPv4Addresses = []string{a}
					}
				}
				n.MACAddress = mac.GenerateNICMAC(cloned.ID, n.ID+"-retry")
			}
			if err2 := s.vmRepo.Create(ctx, cloned); err2 != nil {
				return nil, fmt.Errorf("create cloned vm: %w", err2)
			}
		} else {
			return nil, fmt.Errorf("create cloned vm: %w", err)
		}
	}

	eventData, _ := json.Marshal(map[string]string{
		"source_vm_id": id,
		"cloned_vm_id": cloned.ID,
	})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "cloned", cloned.ID, eventData)
	}

	return cloned, nil
}

func (s *Service) RestoreSnapshot(ctx context.Context, snapshotID string) (*domain.VirtualMachine, error) {
	snap, err := s.snapRepo.GetByID(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("get snapshot: %w", err)
	}
	if snap == nil {
		return nil, fmt.Errorf("snapshot not found")
	}

	vm, err := s.vmRepo.GetByID(ctx, snap.VMID)
	if err != nil {
		return nil, fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return nil, fmt.Errorf("vm not found")
	}

	if err := s.vmRepo.UpdateStatus(ctx, vm.ID, domain.VMStatusStopped); err != nil {
		return nil, fmt.Errorf("stop vm: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{
		"vm_id":       vm.ID,
		"snapshot_id": snapshotID,
	})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "restoring", vm.ID, eventData)
	}

	if err := s.vmRepo.UpdateStatus(ctx, vm.ID, domain.VMStatusRunning); err != nil {
		return nil, fmt.Errorf("restart vm: %w", err)
	}

	vm.Status = domain.VMStatusRunning
	return vm, nil
}

func (s *Service) NodeService() *NodeServiceImpl {
	return &NodeServiceImpl{repo: s.nodeRepo, publisher: s.publisher}
}

type NodeServiceImpl struct {
	repo      domain.NodeRepository
	publisher EventPublisher
}

func (ns *NodeServiceImpl) Register(ctx context.Context, req *domain.RegisterNodeRequest) (*domain.Node, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("node name is required")
	}
	if req.Address == "" {
		return nil, fmt.Errorf("node address is required")
	}

	existing, _ := ns.repo.GetByName(ctx, req.Name)
	if existing != nil {
		return nil, fmt.Errorf("node with name %s already exists", req.Name)
	}

	node := &domain.Node{
		ID:              uuid.New().String(),
		Name:            req.Name,
		Address:         req.Address,
		Port:            req.Port,
		Status:          domain.NodeStatusOnline,
		TotalCPUs:       req.Resources.TotalCPUs,
		UsedCPUs:        req.Resources.UsedCPUs,
		TotalMemoryMB:   req.Resources.TotalMemoryMB,
		UsedMemoryMB:    req.Resources.UsedMemoryMB,
		TotalStorageBytes: req.Resources.TotalStorageBytes,
		UsedStorageBytes:  req.Resources.UsedStorageBytes,
		TotalGPUs:       req.Resources.TotalGPUs,
		UsedGPUs:        req.Resources.UsedGPUs,
		Labels:          req.Labels,
		Metadata:        make(map[string]string),
	}

	if err := ns.repo.Create(ctx, node); err != nil {
		return nil, fmt.Errorf("create node: %w", err)
	}

	if ns.publisher != nil {
		eventData, _ := json.Marshal(map[string]string{"node_id": node.ID})
		_ = ns.publisher.PublishNodeEvent(ctx, "registered", node.ID, eventData)
	}

	return node, nil
}

func (ns *NodeServiceImpl) GetNode(ctx context.Context, id string) (*domain.Node, error) {
	return ns.repo.GetByID(ctx, id)
}

func (ns *NodeServiceImpl) ListNodes(ctx context.Context, filter domain.NodeFilter) ([]*domain.Node, int, error) {
	return ns.repo.List(ctx, filter)
}

func (ns *NodeServiceImpl) UpdateNode(ctx context.Context, id string, labels map[string]string) (*domain.Node, error) {
	node, err := ns.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get node: %w", err)
	}
	if node == nil {
		return nil, fmt.Errorf("node not found")
	}

	node.Labels = labels

	if err := ns.repo.Update(ctx, node); err != nil {
		return nil, fmt.Errorf("update node: %w", err)
	}

	return node, nil
}

func (ns *NodeServiceImpl) Drain(ctx context.Context, id string, timeout int32) error {
	node, err := ns.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}
	if node == nil {
		return fmt.Errorf("node not found")
	}

	if err := ns.repo.UpdateStatus(ctx, id, domain.NodeStatusDraining); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	return nil
}

func (ns *NodeServiceImpl) Heartbeat(ctx context.Context, nodeID string, resources domain.NodeResources) error {
	return ns.repo.UpdateHeartbeat(ctx, nodeID, resources)
}

func (s *Service) ListImages(ctx context.Context, filter domain.ImageFilter) ([]*domain.Image, int, error) {
	return s.imageRepo.List(ctx, filter)
}

func (s *Service) GetImage(ctx context.Context, id string) (*domain.Image, error) {
	return s.imageRepo.GetByID(ctx, id)
}

func (s *Service) CreateImage(ctx context.Context, img *domain.Image) error {
	if img.Name == "" {
		return fmt.Errorf("image name is required")
	}
	if img.OS == "" {
		return fmt.Errorf("os is required")
	}
	if img.Architecture == "" {
		return fmt.Errorf("architecture is required")
	}

	img.ID = uuid.New().String()
	img.Status = domain.ImageStatusDownloading

	if err := s.imageRepo.Create(ctx, img); err != nil {
		return fmt.Errorf("create image: %w", err)
	}

	return nil
}

func (s *Service) DeleteImage(ctx context.Context, id string) error {
	img, err := s.imageRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get image: %w", err)
	}
	if img == nil {
		return fmt.Errorf("image not found")
	}

	if err := s.imageRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete image: %w", err)
	}

	return nil
}

func (s *Service) PullOfficialImage(ctx context.Context, osName, version, arch string) (*domain.Image, error) {
	if s.puller == nil {
		return nil, fmt.Errorf("image puller not configured")
	}
	return s.puller.PullOfficialImage(ctx, osName, version, arch)
}

func (s *Service) ImportFromURL(ctx context.Context, req *domain.ImportImageRequest) (*domain.Image, error) {
	if s.puller == nil {
		return nil, fmt.Errorf("image puller not configured")
	}
	return s.puller.ImportFromURL(ctx, req)
}

func (s *Service) ListOfficialImages(ctx context.Context) ([]domain.OfficialImage, error) {
	return catalog.List(), nil
}

func (s *Service) GetOfficialImage(ctx context.Context, osName, version, arch string) (*domain.OfficialImage, error) {
	return catalog.Find(osName, version, arch)
}



func (s *Service) CreateFlavor(ctx context.Context, req *domain.CreateFlavorRequest) (*domain.Flavor, error) {
	if s.flavorRepo == nil {
		return nil, fmt.Errorf("flavor repository not configured")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("flavor name is required")
	}
	if req.VCpus < 1 {
		return nil, fmt.Errorf("vcpus must be at least 1")
	}
	if req.MemoryMB < 128 {
		return nil, fmt.Errorf("memory must be at least 128 MB")
	}
	if req.DiskGB < 1 {
		return nil, fmt.Errorf("disk_gb must be at least 1")
	}
	existing, _ := s.flavorRepo.GetByName(ctx, req.Name)
	if existing != nil {
		return nil, fmt.Errorf("flavor with name %q already exists", req.Name)
	}
	f := &domain.Flavor{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		VCpus:       req.VCpus,
		MemoryMB:    req.MemoryMB,
		DiskGB:      req.DiskGB,
	}
	if err := s.flavorRepo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("create flavor: %w", err)
	}
	return f, nil
}

func (s *Service) GetFlavor(ctx context.Context, id string) (*domain.Flavor, error) {
	if s.flavorRepo == nil {
		return nil, fmt.Errorf("flavor repository not configured")
	}
	return s.flavorRepo.GetByID(ctx, id)
}

func (s *Service) ListFlavors(ctx context.Context) ([]*domain.Flavor, error) {
	if s.flavorRepo == nil {
		return nil, fmt.Errorf("flavor repository not configured")
	}
	return s.flavorRepo.List(ctx)
}

func (s *Service) DeleteFlavor(ctx context.Context, id string) error {
	if s.flavorRepo == nil {
		return fmt.Errorf("flavor repository not configured")
	}
	f, err := s.flavorRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if f == nil {
		return fmt.Errorf("flavor not found")
	}
	return s.flavorRepo.Delete(ctx, id)
}

func hasPrimary(nics []domain.CreateNetworkInterfaceRequest) bool {
	for _, n := range nics {
		if n.IsPrimary {
			return true
		}
	}
	return false
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "unique") || strings.Contains(msg, "idx_vm_nics")
}



func fingerprintPublicKey(pubKey string) string {
	parts := strings.Fields(pubKey)
	if len(parts) < 2 {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {

		decoded, err = base64.RawStdEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	sum := sha256.Sum256(decoded)
	return fmt.Sprintf("%x", sum[:])[:16]
}

func (s *Service) CreateKeypair(ctx context.Context, req *domain.CreateKeypairRequest) (*domain.Keypair, error) {
	if s.keypairRepo == nil {
		return nil, fmt.Errorf("keypair repository not configured")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("keypair name is required")
	}
	if req.PublicKey == "" {
		return nil, fmt.Errorf("public_key is required")
	}
	if !strings.Contains(req.PublicKey, "ssh-") {
		return nil, fmt.Errorf("invalid public key format")
	}
	orgID := req.OrganizationID
	if orgID == "" || orgID == "default" {
		orgID = "00000000-0000-0000-0000-000000000000"
	}
	existing, _ := s.keypairRepo.GetByName(ctx, req.Name, orgID)
	if existing != nil {
		return nil, fmt.Errorf("keypair with name %q already exists", req.Name)
	}
	k := &domain.Keypair{
		ID:             uuid.New().String(),
		Name:           req.Name,
		PublicKey:      req.PublicKey,
		Fingerprint:    fingerprintPublicKey(req.PublicKey),
		OrganizationID: orgID,
	}
	if err := s.keypairRepo.Create(ctx, k); err != nil {
		return nil, fmt.Errorf("create keypair: %w", err)
	}
	return k, nil
}

func (s *Service) GetKeypair(ctx context.Context, id string) (*domain.Keypair, error) {
	if s.keypairRepo == nil {
		return nil, fmt.Errorf("keypair repository not configured")
	}
	return s.keypairRepo.GetByID(ctx, id)
}

func (s *Service) ListKeypairs(ctx context.Context, orgID string) ([]*domain.Keypair, error) {
	if s.keypairRepo == nil {
		return nil, fmt.Errorf("keypair repository not configured")
	}
	return s.keypairRepo.List(ctx, orgID)
}

func (s *Service) DeleteKeypair(ctx context.Context, id string) error {
	if s.keypairRepo == nil {
		return fmt.Errorf("keypair repository not configured")
	}
	k, err := s.keypairRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if k == nil {
		return fmt.Errorf("keypair not found")
	}
	return s.keypairRepo.Delete(ctx, id)
}

func (s *Service) ListBackups(ctx context.Context, filter domain.BackupFilter) ([]*domain.Backup, int, error) {
	return s.backupRepo.List(ctx, filter)
}

func (s *Service) GetBackup(ctx context.Context, id string) (*domain.Backup, error) {
	return s.backupRepo.GetByID(ctx, id)
}

func (s *Service) CreateBackup(ctx context.Context, backup *domain.Backup) error {
	if backup.VMID == "" {
		return fmt.Errorf("vm_id is required")
	}
	if backup.Name == "" {
		return fmt.Errorf("backup name is required")
	}

	vm, err := s.vmRepo.GetByID(ctx, backup.VMID)
	if err != nil {
		return fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return fmt.Errorf("vm not found")
	}

	backup.ID = uuid.New().String()
	backup.OrganizationID = vm.OrganizationID
	backup.Status = domain.BackupStatusCreating

	if err := s.backupRepo.Create(ctx, backup); err != nil {
		return fmt.Errorf("create backup: %w", err)
	}

	return nil
}

func (s *Service) DeleteBackup(ctx context.Context, id string) error {
	b, err := s.backupRepo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get backup: %w", err)
	}
	if b == nil {
		return fmt.Errorf("backup not found")
	}

	if err := s.backupRepo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete backup: %w", err)
	}

	return nil
}

func (s *Service) RestoreBackup(ctx context.Context, backupID, vmID string) error {
	b, err := s.backupRepo.GetByID(ctx, backupID)
	if err != nil {
		return fmt.Errorf("get backup: %w", err)
	}
	if b == nil {
		return fmt.Errorf("backup not found")
	}

	vm, err := s.vmRepo.GetByID(ctx, vmID)
	if err != nil {
		return fmt.Errorf("get vm: %w", err)
	}
	if vm == nil {
		return fmt.Errorf("vm not found")
	}

	if err := s.vmRepo.UpdateStatus(ctx, vmID, domain.VMStatusStopped); err != nil {
		return fmt.Errorf("stop vm: %w", err)
	}

	eventData, _ := json.Marshal(map[string]string{
		"vm_id":      vmID,
		"backup_id":  backupID,
	})
	if s.publisher != nil {
		_ = s.publisher.PublishVMEvent(ctx, "restoring_backup", vmID, eventData)
	}

	if err := s.vmRepo.UpdateStatus(ctx, vmID, domain.VMStatusRunning); err != nil {
		return fmt.Errorf("restart vm: %w", err)
	}

	return nil
}
