package metrics

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rymelabs/rymevisor/internal/hostinfo"
)


type SystemMetrics struct {
	Timestamp int64         `json:"timestamp"`
	CPU       CPUMetrics    `json:"cpu"`
	Memory    MemoryMetrics `json:"memory"`
	Disk      DiskMetrics   `json:"disk"`
	Network   NetworkMetrics `json:"network"`
	Load      LoadMetrics   `json:"load"`
	Uptime    int64         `json:"uptime_seconds"`
}

type CPUMetrics struct {
	UsagePercent float64   `json:"usage_percent"`
	Cores        int32     `json:"cores"`
	LoadAvg      []float64 `json:"load_avg"`
}

type MemoryMetrics struct {
	TotalMB      int64   `json:"total_mb"`
	UsedMB       int64   `json:"used_mb"`
	FreeMB       int64   `json:"free_mb"`
	AvailableMB  int64   `json:"available_mb"`
	UsagePercent float64 `json:"usage_percent"`
	SwapTotalMB  int64   `json:"swap_total_mb"`
	SwapUsedMB   int64   `json:"swap_used_mb"`
}

type DiskMetrics struct {
	TotalGB      int64   `json:"total_gb"`
	UsedGB       int64   `json:"used_gb"`
	FreeGB       int64   `json:"free_gb"`
	UsagePercent float64 `json:"usage_percent"`
	ReadBytes    int64   `json:"read_bytes"`
	WriteBytes   int64   `json:"write_bytes"`
	ReadIOPS     int64   `json:"read_iops"`
	WriteIOPS    int64   `json:"write_iops"`
}

type NetworkMetrics struct {
	Interfaces []NetInterface `json:"interfaces"`
	TotalRxBytes int64        `json:"total_rx_bytes"`
	TotalTxBytes int64        `json:"total_tx_bytes"`
	TotalRxPackets int64      `json:"total_rx_packets"`
	TotalTxPackets int64      `json:"total_tx_packets"`
	TotalRxMbps  float64      `json:"total_rx_mbps"`
	TotalTxMbps  float64      `json:"total_tx_mbps"`
}

type NetInterface struct {
	Name      string  `json:"name"`
	RxBytes   int64   `json:"rx_bytes"`
	TxBytes   int64   `json:"tx_bytes"`
	RxPackets int64   `json:"rx_packets"`
	TxPackets int64   `json:"tx_packets"`
	RxErrors  int64   `json:"rx_errors"`
	TxErrors  int64   `json:"tx_errors"`
	RxMbps    float64 `json:"rx_mbps"`
	TxMbps    float64 `json:"tx_mbps"`
}

type LoadMetrics struct {
	Avg1  float64 `json:"avg_1"`
	Avg5  float64 `json:"avg_5"`
	Avg15 float64 `json:"avg_15"`
	RunningProcesses int `json:"running_processes"`
	TotalProcesses   int `json:"total_processes"`
}


type VMMetrics struct {
	VMID        string  `json:"vm_id"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	VCpus       int32   `json:"vcpus"`
	MemoryMB    int64   `json:"memory_mb"`
	CPUUsagePercent float64 `json:"cpu_usage_percent"`
	MemoryUsageMB   int64   `json:"memory_usage_mb"`
	MemoryUsagePercent float64 `json:"memory_usage_percent"`
	Disk      VMDiskMetrics    `json:"disk"`
	Network   VMNetworkMetrics `json:"network"`
	UptimeSeconds int64 `json:"uptime_seconds"`
	Timestamp int64 `json:"timestamp"`
	PID       int   `json:"pid,omitempty"`
}

type VMDiskMetrics struct {
	SizeGB     int64 `json:"size_gb"`
	UsedGB     int64 `json:"used_gb"`
	ReadBytes  int64 `json:"read_bytes"`
	WriteBytes int64 `json:"write_bytes"`
}

type VMNetworkMetrics struct {
	RxBytes int64   `json:"rx_bytes"`
	TxBytes int64   `json:"tx_bytes"`
	RxMbps  float64 `json:"rx_mbps"`
	TxMbps  float64 `json:"tx_mbps"`
}

type Collector struct {
	mu sync.Mutex
	prevCPU struct {
		idle, total int64
		time time.Time
	}
	prevNet map[string]NetSample
	prevDisk struct {
		readBytes, writeBytes, readIOPS, writeIOPS int64
		time time.Time
	}
}

type NetSample struct {
	RxBytes, TxBytes, RxPackets, TxPackets int64
	Time time.Time
}

func NewCollector() *Collector {
	return &Collector{
		prevNet: make(map[string]NetSample),
	}
}

func (c *Collector) CollectSystem() (*SystemMetrics, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	m := &SystemMetrics{
		Timestamp: now.Unix(),
	}


	cores, _ := readCPUCores()
	m.CPU.Cores = cores
	usage, _ := c.readCPUUsage()
	m.CPU.UsagePercent = usage
	if load, err := readLoadAvg(); err == nil {
		m.CPU.LoadAvg = load
		m.Load.Avg1, m.Load.Avg5, m.Load.Avg15 = load[0], load[1], load[2]
	}
	if procs, total, err := readLoadProcs(); err == nil {
		m.Load.RunningProcesses = procs
		m.Load.TotalProcesses = total
	}


	if mem, err := readMemory(); err == nil {
		m.Memory = *mem
	}


	if disk, err := readDisk(); err == nil {

		if rb, wb, ri, wi, err := readDiskStats(); err == nil {

			if !c.prevDisk.time.IsZero() {
				elapsed := now.Sub(c.prevDisk.time).Seconds()
				if elapsed > 0 {
					_ = elapsed
				}
			}
			disk.ReadBytes = rb
			disk.WriteBytes = wb
			disk.ReadIOPS = ri
			disk.WriteIOPS = wi
			c.prevDisk.readBytes = rb
			c.prevDisk.writeBytes = wb
			c.prevDisk.readIOPS = ri
			c.prevDisk.writeIOPS = wi
			c.prevDisk.time = now
		}
		m.Disk = *disk
	}


	if netMetrics, err := c.readNetwork(); err == nil {
		m.Network = *netMetrics
	}


	if up, err := readUptime(); err == nil {
		m.Uptime = up
	}

	return m, nil
}

func (c *Collector) CollectVM(vmID, name, status string, vcpus int32, memoryMB int64) (*VMMetrics, error) {
	m := &VMMetrics{
		VMID:     vmID,
		Name:     name,
		Status:   status,
		VCpus:    vcpus,
		MemoryMB: memoryMB,
		Timestamp: time.Now().Unix(),
	}


	pid, err := readVMPid(vmID)
	if err == nil && pid > 0 {
		m.PID = pid
		if cpu, err := readProcessCPU(pid); err == nil {
			m.CPUUsagePercent = cpu
		}
		if mem, err := readProcessMemory(pid); err == nil {
			m.MemoryUsageMB = mem
			if memoryMB > 0 {
				m.MemoryUsagePercent = float64(mem) / float64(memoryMB) * 100
			}
		}
		if up, err := readProcessUptime(pid); err == nil {
			m.UptimeSeconds = up
		}
	}


	if diskPath := vmDiskPath(vmID); diskPath != "" {
		if size, used, err := readVMDiskUsage(diskPath); err == nil {
			m.Disk.SizeGB = size / (1024 * 1024 * 1024)
			m.Disk.UsedGB = used / (1024 * 1024 * 1024)
		}
	}


	if netStats, err := readVMNetwork(vmID); err == nil {
		m.Network = *netStats
	}

	return m, nil
}

func readCPUCores() (int32, error) {
	return hostinfo.ReadCPUCores()
}

func (c *Collector) readCPUUsage() (float64, error) {
	idle, total, err := hostinfo.ReadCPUStat()
	if err != nil {
		return 0, err
	}
	now := time.Now()
	if c.prevCPU.time.IsZero() {
		c.prevCPU.idle = idle
		c.prevCPU.total = total
		c.prevCPU.time = now
		return 0, nil
	}
	elapsed := now.Sub(c.prevCPU.time).Seconds()
	if elapsed < 0.1 {
		return 0, nil
	}
	idleDiff := idle - c.prevCPU.idle
	totalDiff := total - c.prevCPU.total
	c.prevCPU.idle = idle
	c.prevCPU.total = total
	c.prevCPU.time = now
	if totalDiff == 0 {
		return 0, nil
	}
	usage := 100 * (1 - float64(idleDiff)/float64(totalDiff))
	if usage < 0 {
		usage = 0
	}
	if usage > 100 {
		usage = 100
	}
	return usage, nil
}

func readLoadAvg() ([]float64, error) {
	return hostinfo.ReadLoadAvg()
}

func readLoadProcs() (int, int, error) {
	return hostinfo.ReadLoadProcs()
}

func readMemory() (*MemoryMetrics, error) {
	mi, err := hostinfo.ReadMemoryInfo()
	if err != nil {
		return nil, err
	}
	return &MemoryMetrics{
		TotalMB:      mi.TotalMB,
		FreeMB:       mi.FreeMB,
		AvailableMB:  mi.AvailableMB,
		UsedMB:       mi.UsedMB,
		SwapTotalMB:  mi.SwapTotalMB,
		SwapUsedMB:   mi.SwapUsedMB,
		UsagePercent: mi.UsagePercent,
	}, nil
}

func readDisk() (*DiskMetrics, error) {
	di, err := hostinfo.ReadDisk("/")
	if err != nil {
		return nil, err
	}
	return &DiskMetrics{
		TotalGB:      di.TotalGB,
		FreeGB:       di.FreeGB,
		UsedGB:       di.UsedGB,
		UsagePercent: di.UsagePercent,
	}, nil
}

func readDiskStats() (readBytes, writeBytes, readIOPS, writeIOPS int64, err error) {
	info, err := hostinfo.ReadDiskStats()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return info.ReadBytes, info.WriteBytes, info.ReadIOPS, info.WriteIOPS, nil
}

func (c *Collector) readNetwork() (*NetworkMetrics, error) {
	ifaces, err := hostinfo.ReadNetwork()
	if err != nil {
		return nil, err
	}
	var interfaces []NetInterface
	var totalRx, totalTx, totalRxP, totalTxP int64
	now := time.Now()
	for _, iface := range ifaces {
		rxBytes := iface.RxBytes
		rxPackets := iface.RxPackets
		rxErrors := iface.RxErrors
		txBytes := iface.TxBytes
		txPackets := iface.TxPackets
		txErrors := iface.TxErrors
		name := iface.Name

		var rxMbps, txMbps float64
		if prev, ok := c.prevNet[name]; ok {
			elapsed := now.Sub(prev.Time).Seconds()
			if elapsed > 0 {
				rxMbps = float64(rxBytes-prev.RxBytes) * 8 / elapsed / 1e6
				txMbps = float64(txBytes-prev.TxBytes) * 8 / elapsed / 1e6
			}
		}
		c.prevNet[name] = NetSample{RxBytes: rxBytes, TxBytes: txBytes, RxPackets: rxPackets, TxPackets: txPackets, Time: now}

		totalRx += rxBytes
		totalTx += txBytes
		totalRxP += rxPackets
		totalTxP += txPackets

		interfaces = append(interfaces, NetInterface{
			Name: name, RxBytes: rxBytes, TxBytes: txBytes,
			RxPackets: rxPackets, TxPackets: txPackets,
			RxErrors: rxErrors, TxErrors: txErrors,
			RxMbps: rxMbps, TxMbps: txMbps,
		})
	}

	var totalRxMbps, totalTxMbps float64
	if prevTotal, ok := c.prevNet["__total__"]; ok {
		elapsed := now.Sub(prevTotal.Time).Seconds()
		if elapsed > 0 {
			totalRxMbps = float64(totalRx-prevTotal.RxBytes) * 8 / elapsed / 1e6
			totalTxMbps = float64(totalTx-prevTotal.TxBytes) * 8 / elapsed / 1e6
		}
	}
	c.prevNet["__total__"] = NetSample{RxBytes: totalRx, TxBytes: totalTx, Time: now}

	return &NetworkMetrics{
		Interfaces:     interfaces,
		TotalRxBytes:   totalRx,
		TotalTxBytes:   totalTx,
		TotalRxPackets: totalRxP,
		TotalTxPackets: totalTxP,
		TotalRxMbps:    totalRxMbps,
		TotalTxMbps:    totalTxMbps,
	}, nil
}

func readUptime() (int64, error) {
	return hostinfo.ReadUptime()
}

func readVMPid(vmID string) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/var/lib/rymevisor/vms/%s/qemu.pid", vmID))
	if err != nil {
		data, err = os.ReadFile(fmt.Sprintf(".dev-logs/%s.pid", vmID))
		if err != nil {
			return 0, err
		}
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, nil
}

func readProcessCPU(pid int) (float64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 22 {
		return 0, fmt.Errorf("invalid stat")
	}
	utime, _ := strconv.ParseInt(fields[13], 10, 64)
	stime, _ := strconv.ParseInt(fields[14], 10, 64)
	starttime, _ := strconv.ParseInt(fields[21], 10, 64)
	uptimeData, _ := os.ReadFile("/proc/uptime")
	uptimeFields := strings.Fields(string(uptimeData))
	uptime, _ := strconv.ParseFloat(uptimeFields[0], 64)
	clkTck := 100.0
	cpuTime := float64(utime+stime) / clkTck
	elapsed := uptime - float64(starttime)/clkTck
	if elapsed <= 0 {
		return 0, nil
	}
	usage := 100 * cpuTime / elapsed
	if usage > 100 {
		usage = 100
	}
	return usage, nil
}

func readProcessMemory(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return kb / 1024, nil
			}
		}
	}
	return 0, fmt.Errorf("VmRSS not found")
}

func readProcessUptime(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 22 {
		return 0, fmt.Errorf("invalid stat")
	}
	starttime, _ := strconv.ParseInt(fields[21], 10, 64)
	uptimeData, _ := os.ReadFile("/proc/uptime")
	uptimeFields := strings.Fields(string(uptimeData))
	uptime, _ := strconv.ParseFloat(uptimeFields[0], 64)
	clkTck := 100.0
	elapsed := uptime - float64(starttime)/clkTck
	return int64(elapsed), nil
}

func vmDiskPath(vmID string) string {
	candidates := []string{
		fmt.Sprintf("/var/lib/rymevisor/vms/%s/root.qcow2", vmID),
		fmt.Sprintf(".dev-logs/%s.qcow2", vmID),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func readVMDiskUsage(path string) (size, used int64, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	size = fi.Size()
	used = size
	return size, used, nil
}

func readVMNetwork(vmID string) (*VMNetworkMetrics, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	var rx, tx int64
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "tap") || strings.HasPrefix(line, "vnet") || strings.HasPrefix(line, "br-") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			fields := strings.Fields(strings.TrimSpace(parts[1]))
			if len(fields) < 10 {
				continue
			}
			rxB, _ := strconv.ParseInt(fields[0], 10, 64)
			txB, _ := strconv.ParseInt(fields[8], 10, 64)
			rx += rxB
			tx += txB
		}
	}
	return &VMNetworkMetrics{RxBytes: rx, TxBytes: tx}, nil
}
