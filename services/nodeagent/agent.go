package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/rymelabs/rymevisor/internal/hostinfo"
	"github.com/rymelabs/rymevisor/internal/ipam"
	"github.com/rymelabs/rymevisor/internal/mac"
	inats "github.com/rymelabs/rymevisor/internal/nats"
	"github.com/rymelabs/rymevisor/services/controlplane/domain"
	"github.com/rymelabs/rymevisor/services/nodeagent/cloudinit"
	"github.com/rymelabs/rymevisor/services/nodeagent/qemu"
	"go.uber.org/zap"
)

type Agent struct {
	nodeID      string
	hostname    string
	qemu        *qemu.Manager
	js          jetstream.JetStream
	logger      *zap.Logger
	baseDir     string
	heartbeatCh chan struct{}
}

type Heartbeat struct {
	NodeID        string  `json:"node_id"`
	Timestamp     int64   `json:"timestamp"`
	TotalCPUs     int32   `json:"total_cpus"`
	UsedCPUs      int32   `json:"used_cpus"`
	TotalMemoryMB int64   `json:"total_memory_mb"`
	UsedMemoryMB  int64   `json:"used_memory_mb"`
	VMs           []VMInfo `json:"vms"`
}

type VMInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	CPUs   int32  `json:"cpus"`
	Memory int64  `json:"memory_mb"`
}

type VMCommand struct {
	Action string          `json:"action"`
	VMID   string          `json:"vm_id"`
	Config *VMStartConfig  `json:"config,omitempty"`
	Force  bool            `json:"force,omitempty"`
}

type NICConfig struct {
	NetworkID  string `json:"network_id"`
	MACAddress string `json:"mac_address"`
	IPAddress  string `json:"ip_address"`
	Gateway    string `json:"gateway,omitempty"`
	Bridge     string `json:"bridge,omitempty"`
	Name       string `json:"name,omitempty"`
}

type VMStartConfig struct {
	Name              string      `json:"name"`
	VCPUs             int32       `json:"vcpus"`
	MemoryMB          int64       `json:"memory_mb"`
	DiskPath          string      `json:"disk_path"`
	MACAddress        string      `json:"mac_address"`
	SSHKey            string      `json:"ssh_key,omitempty"`
	ImageURL          string      `json:"image_url,omitempty"`
	ImageID           string      `json:"image_id,omitempty"`
	ImagePath         string      `json:"image_path,omitempty"`
	SizeBytes         int64       `json:"size_bytes,omitempty"`
	MachineType       string      `json:"machine_type,omitempty"`
	NetworkInterfaces []NICConfig `json:"network_interfaces,omitempty"`
}

type VMCommandResult struct {
	VMID    string `json:"vm_id"`
	Action  string `json:"action"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
}

func NewAgent(nodeID, hostname string, js jetstream.JetStream, logger *zap.Logger) *Agent {
	baseDir := "/var/lib/rymevisor/vms"

	return &Agent{
		nodeID:      nodeID,
		hostname:    hostname,
		qemu:        qemu.NewManager(baseDir),
		js:          js,
		logger:      logger,
		baseDir:     baseDir,
		heartbeatCh: make(chan struct{}, 1),
	}
}

func (a *Agent) ImagesDir() string {
	return "/var/lib/rymevisor/images"
}

func (a *Agent) StartVM(ctx context.Context, vmID string, cfg *VMStartConfig) error {
	if cfg == nil {
		return fmt.Errorf("vm config is required")
	}

	vmDir := filepath.Join(a.baseDir, vmID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		return fmt.Errorf("create vm dir: %w", err)
	}

	diskPath := cfg.DiskPath
	if diskPath == "" {
		diskPath = filepath.Join(vmDir, "root.qcow2")
		if _, err := os.Stat(diskPath); os.IsNotExist(err) {
			imagePath := ""
			if cfg.ImagePath != "" {
				imagePath = cfg.ImagePath
			} else if cfg.ImageID != "" {
				candidate := filepath.Join(a.ImagesDir(), cfg.ImageID+".qcow2")
				if _, err := os.Stat(candidate); err == nil {
					imagePath = candidate
				} else {
					if _, err := os.Stat(filepath.Join(a.ImagesDir(), cfg.ImageID+".raw")); err == nil {
						imagePath = filepath.Join(a.ImagesDir(), cfg.ImageID+".raw")
					}
				}
			} else if cfg.ImageURL != "" {
				a.logger.Warn("image_url provided but not handled on node-agent, creating empty disk", zap.String("url", cfg.ImageURL))
			}
			sizeBytes := cfg.SizeBytes
			if sizeBytes == 0 {
				sizeBytes = 20 * 1024 * 1024 * 1024
			}
			var path string
			var err error
			if imagePath != "" {
				path, err = a.qemu.CreateDiskFromImage(ctx, vmID, "root", sizeBytes, imagePath)
				a.logger.Info("creating disk from image", zap.String("image", imagePath), zap.String("disk", path))
			} else {
				path, err = a.qemu.CreateDisk(ctx, vmID, "root", sizeBytes)
			}
			if err != nil {
				return fmt.Errorf("create root disk: %w", err)
			}
			diskPath = path
		}
	}

	sshKey := cfg.SSHKey
	if sshKey == "" {
		sshKeyBytes, err := os.ReadFile(filepath.Join(vmDir, "authorized_keys"))
		if err == nil {
			sshKey = strings.TrimSpace(string(sshKeyBytes))
		}
	}


	var nics []NICConfig
	if len(cfg.NetworkInterfaces) > 0 {

		nics = cfg.NetworkInterfaces

		for i := range nics {
			if nics[i].Bridge == "" {
				br, err := ipam.BridgeForNetwork(nics[i].NetworkID)
				if err != nil {
					return fmt.Errorf("bridge for network %s: %w", nics[i].NetworkID, err)
				}
				nics[i].Bridge = br
			}
			if nics[i].Gateway == "" && nics[i].IPAddress != "" {
				if gw, err := gatewayFromCIDR(nics[i].IPAddress); err == nil {
					nics[i].Gateway = gw
				}
			}
			if nics[i].Name == "" {
				nics[i].Name = fmt.Sprintf("ens%d", 3+i)
			}
			if nics[i].MACAddress == "" {
				nics[i].MACAddress = mac.GenerateNICMAC(vmID, fmt.Sprintf("nic-%d", i))
			}

			if err := qemu.EnsureBridge(nics[i].Bridge, nics[i].Gateway, nics[i].IPAddress); err != nil {
				a.logger.Warn("ensure bridge failed (non-root ?)", zap.Error(err), zap.String("bridge", nics[i].Bridge))
			}
		}
	} else {
		return fmt.Errorf("no network interfaces provided")
	}


	var netIfaces []cloudinit.NetworkInterface
	for _, n := range nics {
		cidr := n.IPAddress
		if cidr == "" {
			continue
		}
		netIfaces = append(netIfaces, cloudinit.NetworkInterface{
			Name:        n.Name,
			Addresses:   []string{cidr},
			Gateway:     n.Gateway,
			Nameservers: []string{"8.8.8.8", "8.8.4.4"},
		})
	}
	if len(netIfaces) == 0 {
		return fmt.Errorf("no network interfaces with IP addresses")
	}
	netConfigBytes := cloudinit.GenerateNetworkConfig(cloudinit.NetworkConfig{Interfaces: netIfaces})

	metaData := cloudinit.GenerateMetaData(cfg.Name)
	userData := cloudinit.GenerateUserDataFromString(cfg.Name, sshKey, "ubuntu", "")

	cloudInitDir := filepath.Join(vmDir, "cloud-init")
	isoPath, err := cloudinit.GenerateISO(ctx, cloudInitDir, metaData, userData, netConfigBytes)
	if err != nil {
		return fmt.Errorf("generate cloud-init: %w", err)
	}

	qmpSocket := filepath.Join(vmDir, "qmp.sock")
	monitorSocket := filepath.Join(vmDir, "monitor.sock")

	var qemuNICs []qemu.NIC
	for _, n := range nics {
		qemuNICs = append(qemuNICs, qemu.NIC{Bridge: n.Bridge, MACAddress: n.MACAddress})
	}
	if len(qemuNICs) == 0 {
		return fmt.Errorf("no NICs for QEMU config")
	}
	legacyMAC := qemuNICs[0].MACAddress
	qemuCfg := qemu.VMConfig{
		ID:            vmID,
		Name:          cfg.Name,
		VCPUs:         cfg.VCPUs,
		MemoryMB:      cfg.MemoryMB,
		DiskPath:      diskPath,
		MACAddress:    legacyMAC,
		NICs:          qemuNICs,
		CloudInit:     isoPath,
		QMPSocket:     qmpSocket,
		MonitorSocket: monitorSocket,
		MachineType:   cfg.MachineType,
	}

	if err := a.qemu.StartVM(ctx, qemuCfg); err != nil {
		return fmt.Errorf("start qemu: %w", err)
	}

	a.logger.Info("vm started",
		zap.String("vm_id", vmID),
		zap.String("name", cfg.Name),
		zap.Int32("vcpus", cfg.VCPUs),
		zap.Int64("memory_mb", cfg.MemoryMB),
	)


	persistMAC := legacyMAC
	if persistMAC == "" && len(qemuNICs) > 0 {
		persistMAC = qemuNICs[0].MACAddress
	}
	if err := a.persistVMConfig(vmID, cfg, diskPath, persistMAC, isoPath); err != nil {
		a.logger.Warn("failed to persist vm config for recovery", zap.Error(err), zap.String("vm_id", vmID))
	}

	return nil
}

func (a *Agent) persistVMConfig(vmID string, cfg *VMStartConfig, diskPath, mac, isoPath string) error {
	vmDir := filepath.Join(a.baseDir, vmID)
	persisted := struct {
		VMStartConfig
		DiskPath string `json:"disk_path"`
		MAC      string `json:"mac_address"`
		ISOPath  string `json:"iso_path"`
	}{
		VMStartConfig: *cfg,
		DiskPath:      diskPath,
		MAC:           mac,
		ISOPath:       isoPath,
	}
	data, err := json.Marshal(persisted)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(vmDir, "vm.json"), data, 0644)
}

func (a *Agent) loadVMConfig(vmID string) (*VMStartConfig, error) {
	data, err := os.ReadFile(filepath.Join(a.baseDir, vmID, "vm.json"))
	if err != nil {
		return nil, err
	}
	var wrapper struct {
		VMStartConfig
		DiskPath string `json:"disk_path"`
		MAC      string `json:"mac_address"`
		ISOPath  string `json:"iso_path"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	if wrapper.DiskPath != "" {
		wrapper.VMStartConfig.DiskPath = wrapper.DiskPath
	}
	if wrapper.MAC != "" {
		wrapper.VMStartConfig.MACAddress = wrapper.MAC
	}
	return &wrapper.VMStartConfig, nil
}


func (a *Agent) RecoverVMs(ctx context.Context) error {
	entries, err := os.ReadDir(a.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read baseDir for recovery: %w", err)
	}

	a.logger.Info("starting VM recovery", zap.Int("found_dirs", len(entries)))

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		vmID := entry.Name()
		vmJson := filepath.Join(a.baseDir, vmID, "vm.json")
		if _, err := os.Stat(vmJson); os.IsNotExist(err) {
			continue
		}

		status, err := a.qemu.GetVMStatus(ctx, vmID)
		if err == nil && status == "running" {
			a.logger.Info("VM already running, skipping recovery", zap.String("vm_id", vmID), zap.String("status", status))
			continue
		}

		cfg, err := a.loadVMConfig(vmID)
		if err != nil {
			a.logger.Warn("failed to load vm config for recovery", zap.Error(err), zap.String("vm_id", vmID))
			continue
		}

		a.logger.Info("recovering VM after restart", zap.String("vm_id", vmID), zap.String("name", cfg.Name))
		if err := a.StartVM(ctx, vmID, cfg); err != nil {
			a.logger.Error("failed to recover VM", zap.Error(err), zap.String("vm_id", vmID))
		} else {
			a.logger.Info("VM recovered successfully", zap.String("vm_id", vmID))
		}
	}

	a.logger.Info("VM recovery complete")
	return nil
}

func (a *Agent) StopVM(ctx context.Context, vmID string, force bool) error {
	status, err := a.qemu.GetVMStatus(ctx, vmID)
	if err != nil {
		return fmt.Errorf("get vm status: %w", err)
	}

	if status == "stopped" {
		return nil
	}

	if err := a.qemu.StopVM(ctx, vmID, force); err != nil {
		return fmt.Errorf("stop vm: %w", err)
	}



	_ = os.Remove(filepath.Join(a.baseDir, vmID, "vm.json"))

	a.logger.Info("vm stopped", zap.String("vm_id", vmID), zap.Bool("force", force))
	return nil
}

func (a *Agent) RebootVM(ctx context.Context, vmID string, force bool) error {
	status, err := a.qemu.GetVMStatus(ctx, vmID)
	if err != nil {
		return fmt.Errorf("get vm status: %w", err)
	}

	if status == "stopped" {
		return fmt.Errorf("cannot reboot stopped vm")
	}

	if err := a.qemu.RebootVM(ctx, vmID, force); err != nil {
		return fmt.Errorf("reboot vm: %w", err)
	}

	a.logger.Info("vm rebooted", zap.String("vm_id", vmID), zap.Bool("force", force))
	return nil
}

func (a *Agent) GetVMStatus(ctx context.Context, vmID string) (domain.VMStatus, error) {
	status, err := a.qemu.GetVMStatus(ctx, vmID)
	if err != nil {
		return domain.VMStatusError, err
	}

	switch status {
	case "running":
		return domain.VMStatusRunning, nil
	case "paused":
		return domain.VMStatusPaused, nil
	case "shutdown", "guest-shutdown":
		return domain.VMStatusStopped, nil
	case "reboot":
		return domain.VMStatusRebooting, nil
	default:
		return domain.VMStatusStopped, nil
	}
}

func (a *Agent) SendHeartbeat(ctx context.Context) error {
	heartbeat, err := a.collectHeartbeat(ctx)
	if err != nil {
		return fmt.Errorf("collect heartbeat: %w", err)
	}

	data, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	subject := fmt.Sprintf("heartbeats.%s", a.nodeID)
	_, err = a.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("publish heartbeat: %w", err)
	}

	a.logger.Debug("heartbeat sent",
		zap.String("node_id", a.nodeID),
		zap.Int32("cpus", heartbeat.UsedCPUs),
		zap.Int64("memory_mb", heartbeat.UsedMemoryMB),
	)

	return nil
}

func (a *Agent) CreateDisk(ctx context.Context, vmID, name string, sizeBytes int64) (string, error) {
	path, err := a.qemu.CreateDisk(ctx, vmID, name, sizeBytes)
	if err != nil {
		return "", fmt.Errorf("create disk: %w", err)
	}

	a.logger.Info("disk created",
		zap.String("vm_id", vmID),
		zap.String("name", name),
		zap.Int64("size_bytes", sizeBytes),
	)

	return path, nil
}

func (a *Agent) AttachDisk(ctx context.Context, vmID, diskPath string, slot int) error {
	if err := a.qemu.AttachDisk(ctx, vmID, diskPath, slot); err != nil {
		return fmt.Errorf("attach disk: %w", err)
	}

	a.logger.Info("disk attached",
		zap.String("vm_id", vmID),
		zap.String("disk_path", diskPath),
		zap.Int("slot", slot),
	)

	return nil
}

func (a *Agent) DetachDisk(ctx context.Context, vmID, deviceID string) error {
	if err := a.qemu.DetachDisk(ctx, vmID, deviceID); err != nil {
		return fmt.Errorf("detach disk: %w", err)
	}

	a.logger.Info("disk detached",
		zap.String("vm_id", vmID),
		zap.String("device_id", deviceID),
	)

	return nil
}

func (a *Agent) SubscribeCommands(ctx context.Context) error {
	subject := inats.SubjectForNode(a.nodeID, ">")

	cons, err := a.js.CreateOrUpdateConsumer(ctx, "COMMANDS", jetstream.ConsumerConfig{
		Durable:   fmt.Sprintf("node-agent-%s", a.nodeID),
		AckPolicy: jetstream.AckExplicitPolicy,
		FilterSubject: subject,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	_, err = cons.Consume(func(msg jetstream.Msg) {
		a.handleCommand(ctx, msg)
	})
	if err != nil {
		return fmt.Errorf("subscribe commands: %w", err)
	}

	a.logger.Info("subscribed to commands", zap.String("subject", subject))
	return nil
}

func (a *Agent) handleCommand(ctx context.Context, msg jetstream.Msg) {
	var cmd VMCommand
	if err := json.Unmarshal(msg.Data(), &cmd); err != nil {
		a.logger.Error("failed to unmarshal command", zap.Error(err))
		_ = msg.Nak()
		return
	}

	var result VMCommandResult
	result.VMID = cmd.VMID
	result.Action = cmd.Action

	var err error

	switch cmd.Action {
	case "start":
		err = a.StartVM(ctx, cmd.VMID, cmd.Config)
	case "stop":
		err = a.StopVM(ctx, cmd.VMID, cmd.Force)
	case "reboot":
		err = a.RebootVM(ctx, cmd.VMID, cmd.Force)
	case "create_disk":
		if cmd.Config != nil {
			var path string
			path, err = a.CreateDisk(ctx, cmd.VMID, "disk", 10*1024*1024*1024)
			_ = path
		}
	case "attach_disk":

		if cmd.Config != nil {
			err = a.AttachDisk(ctx, cmd.VMID, cmd.Config.DiskPath, 1)
		}
	case "detach_disk":

		err = a.DetachDisk(ctx, cmd.VMID, cmd.VMID)
	default:
		err = fmt.Errorf("unknown action: %s", cmd.Action)
	}

	if err != nil {
		result.Error = err.Error()
		result.Status = "error"
	} else {
		result.Status = "success"
	}

	resultData, _ := json.Marshal(result)
	resultSubject := fmt.Sprintf("events.vm.%s.%s", cmd.VMID, cmd.Action)
	_, _ = a.js.Publish(ctx, resultSubject, resultData)

	if err != nil {
		_ = msg.Nak()
	} else {
		_ = msg.Ack()
	}
}

func (a *Agent) collectHeartbeat(ctx context.Context) (*Heartbeat, error) {
	totalCPUs, err := readTotalCPUs()
	if err != nil {
		return nil, fmt.Errorf("read total cpus: %w", err)
	}

	usedCPUs, err := readUsedCPUs()
	if err != nil {
		return nil, fmt.Errorf("read used cpus: %w", err)
	}

	totalMem, usedMem, err := readMemory()
	if err != nil {
		return nil, fmt.Errorf("read memory: %w", err)
	}

	vms, err := a.listVMs(ctx)
	if err != nil {
		a.logger.Warn("failed to list vms for heartbeat", zap.Error(err))
		vms = []VMInfo{}
	}

	return &Heartbeat{
		NodeID:        a.nodeID,
		Timestamp:     time.Now().Unix(),
		TotalCPUs:     totalCPUs,
		UsedCPUs:      usedCPUs,
		TotalMemoryMB: totalMem,
		UsedMemoryMB:  usedMem,
		VMs:           vms,
	}, nil
}

func (a *Agent) listVMs(ctx context.Context) ([]VMInfo, error) {
	entries, err := os.ReadDir(a.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []VMInfo{}, nil
		}
		return nil, err
	}

	var vms []VMInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		vmID := entry.Name()
		status, err := a.qemu.GetVMStatus(ctx, vmID)
		if err != nil {
			continue
		}

		vms = append(vms, VMInfo{
			ID:     vmID,
			Status: status,
			CPUs:   1,
			Memory: 512,
		})
	}

	return vms, nil
}

func readTotalCPUs() (int32, error) {
	return hostinfo.ReadCPUCores()
}

func readUsedCPUs() (int32, error) {
	return hostinfo.ReadUsedCPUs()
}

func readMemory() (int64, int64, error) {
	return hostinfo.ReadMemory()
}

func gatewayFromCIDR(cidrOrIP string) (string, error) {
	ip, ipNet, err := net.ParseCIDR(cidrOrIP)
	if err != nil {

		if parsed := net.ParseIP(cidrOrIP); parsed != nil {

			v4 := parsed.To4()
			if v4 == nil {
				return "", fmt.Errorf("not IPv4")
			}
			v4[3] = 1
			return v4.String(), nil
		}
		return "", err
	}

	gw := make(net.IP, len(ipNet.IP))
	copy(gw, ipNet.IP)

	if v4 := gw.To4(); v4 != nil {
		gw = v4



		val := uint32(gw[0])<<24 | uint32(gw[1])<<16 | uint32(gw[2])<<8 | uint32(gw[3])
		val += 1
		gw[0] = byte(val >> 24)
		gw[1] = byte(val >> 16)
		gw[2] = byte(val >> 8)
		gw[3] = byte(val)
		return gw.String(), nil
	}

	_ = ip
	return gw.String(), nil
}
