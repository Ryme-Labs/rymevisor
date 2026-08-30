package qemu

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rymelabs/rymevisor/internal/qcow2"
)

type Manager struct {
	baseDir string
}

type NIC struct {
	Bridge     string
	MACAddress string
}

type VMConfig struct {
	ID            string
	Name          string
	VCPUs         int32
	MemoryMB      int64
	DiskPath      string
	MACAddress    string
	NICs          []NIC
	CloudInit     string
	QMPSocket     string
	MonitorSocket string
	MachineType   string
}

type qmpResponse struct {
	Return json.RawMessage `json:"return,omitempty"`
	Error  *qmpError       `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
}

type qmpError struct {
	Class string `json:"class"`
	Desc  string `json:"desc"`
}

type qmpStatus struct {
	Status  string `json:"status"`
 Running bool   `json:"running"`
}

func NewManager(baseDir string) *Manager {
	return &Manager{baseDir: baseDir}
}

func (m *Manager) vmDir(vmID string) string {
	return filepath.Join(m.baseDir, vmID)
}

func (m *Manager) pidFile(vmID string) string {
	return filepath.Join(m.vmDir(vmID), "qemu.pid")
}

func (m *Manager) StartVM(ctx context.Context, cfg VMConfig) error {
	dir := m.vmDir(cfg.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create vm dir: %w", err)
	}

	nics := cfg.NICs
	if len(nics) == 0 {
		return fmt.Errorf("no NICs provided")
	}


	for _, n := range nics {
		_ = EnsureBridge(n.Bridge, "", "")
	}


	machineType := cfg.MachineType
	if machineType == "" {
		machineType = "q35"
	}
	hasKVM := false
	if _, err := os.Stat("/dev/kvm"); err == nil {
		hasKVM = true
	}

	args := []string{
		"-name", cfg.Name,
		"-smp", strconv.Itoa(int(cfg.VCPUs)),
		"-m", strconv.FormatInt(cfg.MemoryMB, 10),
		"-drive", fmt.Sprintf("file=%s,format=qcow2,cache=none", cfg.DiskPath),
	}

	for i, n := range nics {
		args = append(args,
			"-netdev", fmt.Sprintf("bridge,id=net%d,br=%s", i, n.Bridge),
			"-device", fmt.Sprintf("virtio-net-pci,netdev=net%d,mac=%s", i, n.MACAddress),
		)
	}
	args = append(args,
		"-qmp", fmt.Sprintf("unix:%s,server,nowait", cfg.QMPSocket),
		"-monitor", fmt.Sprintf("unix:%s,server,nowait", cfg.MonitorSocket),
	)
	if hasKVM {
		args = append(args, "-enable-kvm", "-machine", fmt.Sprintf("%s,accel=kvm", machineType))
	} else {
		args = append(args, "-machine", fmt.Sprintf("%s,accel=tcg", machineType))
	}
	args = append(args,
		"-display", "none",
		"-daemonize",
	)

	if cfg.CloudInit != "" {
		args = append(args, "-drive", fmt.Sprintf("file=%s,format=raw,if=virtio", cfg.CloudInit))
	}

	pidPath := m.pidFile(cfg.ID)
	args = append(args, "-pidfile", pidPath)

	cmd := exec.CommandContext(ctx, "qemu-system-x86_64", args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("start qemu: %w: %s", err, string(output))
	}

	if err := m.waitForSocket(cfg.QMPSocket, 10*time.Second); err != nil {
		return fmt.Errorf("wait for qmp socket: %w", err)
	}

	return nil
}

func (m *Manager) StopVM(ctx context.Context, vmID string, force bool) error {
	sock := filepath.Join(m.vmDir(vmID), "qmp.sock")

	if force {
		if err := m.sendQMPCommand(sock, map[string]interface{}{"execute": "quit"}); err != nil {
			return m.killProcess(vmID)
		}
	} else {
		if err := m.sendQMPCommand(sock, map[string]interface{}{"execute": "system_powerdown"}); err != nil {
			return m.killProcess(vmID)
		}
	}

	return m.waitForProcessExit(vmID, 30*time.Second)
}

func (m *Manager) RebootVM(ctx context.Context, vmID string, force bool) error {
	sock := filepath.Join(m.vmDir(vmID), "qmp.sock")

	if force {
		if err := m.sendQMPCommand(sock, map[string]interface{}{"execute": "quit"}); err != nil {
			return m.killProcess(vmID)
		}
		if err := m.waitForProcessExit(vmID, 10*time.Second); err != nil {
			return err
		}
		return fmt.Errorf("force reboot requires restart of qemu process")
	}

	return m.sendQMPCommand(sock, map[string]interface{}{"execute": "system_reset"})
}

func (m *Manager) GetVMStatus(ctx context.Context, vmID string) (string, error) {
	pidPath := m.pidFile(vmID)
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "stopped", nil
		}
		return "", fmt.Errorf("read pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return "", fmt.Errorf("parse pid: %w", err)
	}

	procPath := fmt.Sprintf("/proc/%d/status", pid)
	if _, err := os.Stat(procPath); os.IsNotExist(err) {
		return "stopped", nil
	}

	sock := filepath.Join(m.vmDir(vmID), "qmp.sock")
	raw, err := m.sendQMPCommandWithReturn(sock, map[string]interface{}{"execute": "query-status"})
	if err != nil {
		return "running", nil
	}

	var status qmpStatus
	if err := json.Unmarshal(raw, &status); err != nil {
		return "running", nil
	}

	return status.Status, nil
}

func (m *Manager) CreateDisk(ctx context.Context, vmID, name string, sizeBytes int64) (string, error) {
	return m.CreateDiskFromImage(ctx, vmID, name, sizeBytes, "")
}

func (m *Manager) CreateDiskFromImage(ctx context.Context, vmID, name string, sizeBytes int64, imagePath string) (string, error) {
	dir := m.vmDir(vmID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create vm dir: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("%s.qcow2", name))

	if imagePath != "" {
		if _, err := os.Stat(imagePath); err == nil {
			if err := qcow2.CreateWithBacking(ctx, path, imagePath, sizeBytes); err != nil {
				return "", err
			}
			return path, nil
		}
	}

	if err := qcow2.Create(ctx, path, sizeBytes); err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) AttachDisk(ctx context.Context, vmID, diskPath string, slot int) error {
	sock := filepath.Join(m.vmDir(vmID), "qmp.sock")

	devID := fmt.Sprintf("virtio-blk-pci%d", slot)
	blockNode := fmt.Sprintf("drive%d", slot)

	if err := m.sendQMPCommand(sock, map[string]interface{}{
		"execute": "blockdev-add",
		"arguments": map[string]interface{}{
			"driver": "qcow2",
			"node-name": blockNode,
			"read-only": false,
			"file": map[string]interface{}{
				"driver": "file",
				"filename": diskPath,
			},
		},
	}); err != nil {
		return fmt.Errorf("blockdev-add: %w", err)
	}

	if err := m.sendQMPCommand(sock, map[string]interface{}{
		"execute": "device_add",
		"arguments": map[string]interface{}{
			"driver":     "virtio-blk-pci",
			"id":         devID,
			"drive":      blockNode,
			"bus":        fmt.Sprintf("pci.%d", slot+1),
		},
	}); err != nil {
		return fmt.Errorf("device_add: %w", err)
	}

	return nil
}

func (m *Manager) DetachDisk(ctx context.Context, vmID, deviceID string) error {
	sock := filepath.Join(m.vmDir(vmID), "qmp.sock")

	if err := m.sendQMPCommand(sock, map[string]interface{}{
		"execute": "device_del",
		"arguments": map[string]interface{}{
			"id": deviceID,
		},
	}); err != nil {
		return fmt.Errorf("device_del: %w", err)
	}

	time.Sleep(100 * time.Millisecond)

	if err := m.sendQMPCommand(sock, map[string]interface{}{
		"execute": "blockdev-del",
		"arguments": map[string]interface{}{
			"node-name": deviceID,
		},
	}); err != nil {
		_ = err
	}

	return nil
}

func (m *Manager) sendQMPCommand(socketPath string, cmd map[string]interface{}) error {
	_, err := m.sendQMPCommandWithReturn(socketPath, cmd)
	return err
}

func (m *Manager) sendQMPCommandWithReturn(socketPath string, cmd map[string]interface{}) (json.RawMessage, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial qmp: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("set deadline: %w", err)
	}

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var greeting qmpResponse
	if err := decoder.Decode(&greeting); err != nil {
		return nil, fmt.Errorf("read qmp greeting: %w", err)
	}

	if err := encoder.Encode(map[string]interface{}{"execute": "qmp_capabilities"}); err != nil {
		return nil, fmt.Errorf("send capabilities: %w", err)
	}

	var capResp qmpResponse
	if err := decoder.Decode(&capResp); err != nil {
		return nil, fmt.Errorf("read capabilities response: %w", err)
	}

	if err := encoder.Encode(cmd); err != nil {
		return nil, fmt.Errorf("send command: %w", err)
	}

	var resp qmpResponse
	for {
		if err := decoder.Decode(&resp); err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.Event != "" {
			continue
		}
		break
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("qmp error: %s: %s", resp.Error.Class, resp.Error.Desc)
	}

	return resp.Return, nil
}

func (m *Manager) killProcess(vmID string) error {
	pidPath := m.pidFile(vmID)
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("read pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return fmt.Errorf("parse pid: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process: %w", err)
	}

	return proc.Signal(syscall.SIGKILL)
}

func (m *Manager) waitForProcessExit(vmID string, timeout time.Duration) error {
	pidPath := m.pidFile(vmID)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		pidBytes, err := os.ReadFile(pidPath)
		if err != nil {
			return nil
		}

		pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
		if err != nil {
			return nil
		}

		procPath := fmt.Sprintf("/proc/%d", pid)
		if _, err := os.Stat(procPath); os.IsNotExist(err) {
			os.Remove(pidPath)
			return nil
		}

		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for process to exit")
}

func (m *Manager) waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("unix", path, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for socket %s", path)
}




func EnsureBridge(bridge, gatewayIP, ipCIDR string) error {

	if _, err := exec.Command("ip", "link", "show", bridge).CombinedOutput(); err == nil {

		_, _ = exec.Command("ip", "link", "set", bridge, "up").CombinedOutput()
		return nil
	}


	if out, err := exec.Command("ip", "link", "add", bridge, "type", "bridge").CombinedOutput(); err != nil {

		if strings.Contains(string(out), "exists") {
			return nil
		}

		if strings.Contains(string(out), "Operation not permitted") || strings.Contains(string(out), "permission") {
			return nil
		}
		return fmt.Errorf("create bridge %s: %s", bridge, string(out))
	}

	if _, err := exec.Command("ip", "link", "set", bridge, "up").CombinedOutput(); err != nil {
		return nil
	}


	if gatewayIP != "" {
		prefix := 24
		if ipCIDR != "" {
			if _, ipNet, err := net.ParseCIDR(ipCIDR); err == nil {
				if ones, _ := ipNet.Mask.Size(); ones > 0 {
					prefix = ones
				}
			}
		}
		gwCIDR := fmt.Sprintf("%s/%d", gatewayIP, prefix)
		_, _ = exec.Command("ip", "addr", "add", gwCIDR, "dev", bridge).CombinedOutput()

		_, _ = exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-s", gwCIDR, "-j", "MASQUERADE").CombinedOutput()

		if _, err := exec.Command("iptables", "-t", "nat", "-C", "POSTROUTING", "-s", fmt.Sprintf("%s/%d", ipCIDR, prefix), "-j", "MASQUERADE").CombinedOutput(); err != nil {
			_, _ = exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING", "-s", gwCIDR, "-j", "MASQUERADE").CombinedOutput()
		}

		_, _ = exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").CombinedOutput()
	}

	return nil
}
