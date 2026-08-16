package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
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

type VMStartConfig struct {
	Name       string `json:"name"`
	VCPUs      int32  `json:"vcpus"`
	MemoryMB   int64  `json:"memory_mb"`
	DiskPath   string `json:"disk_path"`
	MACAddress string `json:"mac_address"`
	SSHKey     string `json:"ssh_key,omitempty"`
	ImageURL   string `json:"image_url,omitempty"`
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
			path, err := a.qemu.CreateDisk(ctx, vmID, "root", 20*1024*1024*1024)
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

	metaData := cloudinit.GenerateMetaData(cfg.Name)
	userData := cloudinit.GenerateUserDataFromString(cfg.Name, sshKey, "ubuntu", "")
	netConfig := cloudinit.GenerateNetworkConfigBytes(cfg.Name, []string{"192.168.122.10/24"}, "192.168.122.1", nil)

	cloudInitDir := filepath.Join(vmDir, "cloud-init")
	isoPath, err := cloudinit.GenerateISO(ctx, cloudInitDir, metaData, userData, netConfig)
	if err != nil {
		return fmt.Errorf("generate cloud-init: %w", err)
	}

	qmpSocket := filepath.Join(vmDir, "qmp.sock")
	monitorSocket := filepath.Join(vmDir, "monitor.sock")

	mac := cfg.MACAddress
	if mac == "" {
		mac = generateMAC(vmID)
	}

	qemuCfg := qemu.VMConfig{
		ID:            vmID,
		Name:          cfg.Name,
		VCPUs:         cfg.VCPUs,
		MemoryMB:      cfg.MemoryMB,
		DiskPath:      diskPath,
		MACAddress:    mac,
		CloudInit:     isoPath,
		QMPSocket:     qmpSocket,
		MonitorSocket: monitorSocket,
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

func (a *Agent) GetConsoleURL(ctx context.Context, vmID string) (string, error) {
	monitorSocket := filepath.Join(a.baseDir, vmID, "monitor.sock")
	if _, err := os.Stat(monitorSocket); os.IsNotExist(err) {
		return "", fmt.Errorf("vm not running")
	}

	return fmt.Sprintf("unix://%s", monitorSocket), nil
}

func (a *Agent) SubscribeCommands(ctx context.Context) error {
	subject := fmt.Sprintf("commands.node.%s.>", a.nodeID)

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
		msg.Nak()
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
		// expects disk_path in config
		if cmd.Config != nil {
			err = a.AttachDisk(ctx, cmd.VMID, cmd.Config.DiskPath, 1)
		}
	case "detach_disk":
		// expects device_id - use VMID as placeholder for now
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
		msg.Nak()
	} else {
		msg.Ack()
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
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0, err
	}

	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}

	return int32(count), nil
}

func readUsedCPUs() (int32, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				continue
			}

			user, _ := strconv.ParseInt(fields[1], 10, 64)
			nice, _ := strconv.ParseInt(fields[2], 10, 64)
			system, _ := strconv.ParseInt(fields[3], 10, 64)
			idle, _ := strconv.ParseInt(fields[4], 10, 64)

			total := user + nice + system + idle
			used := user + nice + system

			totalCPUs, _ := readTotalCPUs()
			usage := float64(used) / float64(total) * float64(totalCPUs)
			return int32(usage + 0.5), nil
		}
	}

	return 0, nil
}

func readMemory() (int64, int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}

	var totalKB, availableKB int64

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		val, _ := strconv.ParseInt(fields[1], 10, 64)

		switch fields[0] {
		case "MemTotal:":
			totalKB = val
		case "MemAvailable:":
			availableKB = val
		}
	}

	totalMB := totalKB / 1024
	usedMB := (totalKB - availableKB) / 1024

	return totalMB, usedMB, nil
}

func generateMAC(vmID string) string {
	hash := uint32(0)
	for _, c := range vmID {
		hash = hash*31 + uint32(c)
	}

	return fmt.Sprintf("52:54:%02x:%02x:%02x:%02x",
		(hash>>24)&0xff,
		(hash>>16)&0xff,
		(hash>>8)&0xff,
		hash&0xff,
	)
}
