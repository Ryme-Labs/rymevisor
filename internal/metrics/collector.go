package metrics

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SystemMetrics represents full host metrics
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
	LoadAvg      []float64 `json:"load_avg"` // 1,5,15 min
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

// VMMetrics represents per-VM metrics
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

	// CPU
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

	// Memory
	if mem, err := readMemory(); err == nil {
		m.Memory = *mem
	}

	// Disk
	if disk, err := readDisk(); err == nil {
		// Add I/O stats
		if rb, wb, ri, wi, err := readDiskStats(); err == nil {
			// Calculate delta if we have previous
			if !c.prevDisk.time.IsZero() {
				elapsed := now.Sub(c.prevDisk.time).Seconds()
				if elapsed > 0 {
					// For now, just set absolute, not rate
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

	// Network
	if netMetrics, err := c.readNetwork(); err == nil {
		m.Network = *netMetrics
	}

	// Uptime
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

	// Try to get PID and process stats if VM is running
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

	// Disk stats for VM
	if diskPath := vmDiskPath(vmID); diskPath != "" {
		if size, used, err := readVMDiskUsage(diskPath); err == nil {
			m.Disk.SizeGB = size / (1024 * 1024 * 1024)
			m.Disk.UsedGB = used / (1024 * 1024 * 1024)
		}
	}

	// Network stats for VM tap interface
	if netStats, err := readVMNetwork(vmID); err == nil {
		m.Network = *netStats
	}

	return m, nil
}

func readCPUCores() (int32, error) {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0, err
	}
	count := int32(0)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "processor") {
			count++
		}
	}
	return count, nil
}

func (c *Collector) readCPUUsage() (float64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	var idle, total int64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				break
			}
			for i, f := range fields[1:] {
				v, _ := strconv.ParseInt(f, 10, 64)
				total += v
				if i == 3 { // idle is 4th field (0-indexed 3)
					idle = v
				}
			}
			break
		}
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
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid loadavg")
	}
	var out []float64
	for i := 0; i < 3; i++ {
		v, _ := strconv.ParseFloat(fields[i], 64)
		out = append(out, v)
	}
	return out, nil
}

func readLoadProcs() (int, int, error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return 0, 0, nil
	}
	// field 4 is like "2/123"
	parts := strings.Split(fields[3], "/")
	if len(parts) != 2 {
		return 0, 0, nil
	}
	running, _ := strconv.Atoi(parts[0])
	total, _ := strconv.Atoi(parts[1])
	return running, total, nil
}

func readMemory() (*MemoryMetrics, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return nil, err
	}
	var total, free, available, buffers, cached, swapTotal, swapFree int64
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = val
		case "MemFree:":
			free = val
		case "MemAvailable:":
			available = val
		case "Buffers:":
			buffers = val
		case "Cached:":
			cached = val
		case "SwapTotal:":
			swapTotal = val
		case "SwapFree:":
			swapFree = val
		}
	}
	used := total - free - buffers - cached
	if used < 0 {
		used = total - available
	}
	m := &MemoryMetrics{
		TotalMB:     total / 1024,
		FreeMB:      free / 1024,
		AvailableMB: available / 1024,
		UsedMB:      used / 1024,
		SwapTotalMB: swapTotal / 1024,
		SwapUsedMB:  (swapTotal - swapFree) / 1024,
	}
	if total > 0 {
		m.UsagePercent = float64(used) / float64(total) * 100
	}
	return m, nil
}

func readDisk() (*DiskMetrics, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return nil, err
	}
	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bfree) * int64(stat.Bsize)
	used := total - free
	m := &DiskMetrics{
		TotalGB: total / (1024 * 1024 * 1024),
		FreeGB:  free / (1024 * 1024 * 1024),
		UsedGB:  used / (1024 * 1024 * 1024),
	}
	if total > 0 {
		m.UsagePercent = float64(used) / float64(total) * 100
	}
	return m, nil
}

func readDiskStats() (readBytes, writeBytes, readIOPS, writeIOPS int64, err error) {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		dev := fields[2]
		// Skip loop, ram, etc.
		if strings.HasPrefix(dev, "loop") || strings.HasPrefix(dev, "ram") {
			continue
		}
		// Consider sda, vda, nvme
		if !strings.HasPrefix(dev, "sd") && !strings.HasPrefix(dev, "vd") && !strings.HasPrefix(dev, "nvme") && !strings.HasPrefix(dev, "mmc") {
			continue
		}
		reads, _ := strconv.ParseInt(fields[3], 10, 64)
		sectorsRead, _ := strconv.ParseInt(fields[5], 10, 64)
		writes, _ := strconv.ParseInt(fields[7], 10, 64)
		sectorsWritten, _ := strconv.ParseInt(fields[9], 10, 64)
		readIOPS += reads
		writeIOPS += writes
		readBytes += sectorsRead * 512
		writeBytes += sectorsWritten * 512
	}
	return
}

func (c *Collector) readNetwork() (*NetworkMetrics, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	var interfaces []NetInterface
	var totalRx, totalTx, totalRxP, totalTxP int64
	now := time.Now()
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(parts[1]))
		if len(fields) < 16 {
			continue
		}
		rxBytes, _ := strconv.ParseInt(fields[0], 10, 64)
		rxPackets, _ := strconv.ParseInt(fields[1], 10, 64)
		rxErrors, _ := strconv.ParseInt(fields[2], 10, 64)
		txBytes, _ := strconv.ParseInt(fields[8], 10, 64)
		txPackets, _ := strconv.ParseInt(fields[9], 10, 64)
		txErrors, _ := strconv.ParseInt(fields[10], 10, 64)

		// Calculate Mbps
		var rxMbps, txMbps float64
		if prev, ok := c.prevNet[iface]; ok {
			elapsed := now.Sub(prev.Time).Seconds()
			if elapsed > 0 {
				rxMbps = float64(rxBytes-prev.RxBytes) * 8 / elapsed / 1e6
				txMbps = float64(txBytes-prev.TxBytes) * 8 / elapsed / 1e6
			}
		}
		c.prevNet[iface] = NetSample{RxBytes: rxBytes, TxBytes: txBytes, RxPackets: rxPackets, TxPackets: txPackets, Time: now}

		totalRx += rxBytes
		totalTx += txBytes
		totalRxP += rxPackets
		totalTxP += txPackets

		interfaces = append(interfaces, NetInterface{
			Name: iface, RxBytes: rxBytes, TxBytes: txBytes,
			RxPackets: rxPackets, TxPackets: txPackets,
			RxErrors: rxErrors, TxErrors: txErrors,
			RxMbps: rxMbps, TxMbps: txMbps,
		})
	}

	// Calculate total Mbps from previous total
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
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 1 {
		return 0, fmt.Errorf("invalid uptime")
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return int64(v), nil
}

func readVMPid(vmID string) (int, error) {
	data, err := os.ReadFile(fmt.Sprintf("/var/lib/rymevisor/vms/%s/qemu.pid", vmID))
	if err != nil {
		// Try .dev path
		data, err = os.ReadFile(fmt.Sprintf(".dev-logs/%s.pid", vmID))
		if err != nil {
			return 0, err
		}
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid, nil
}

func readProcessCPU(pid int) (float64, error) {
	// Read /proc/<pid>/stat and calculate CPU usage
	// For simplicity, return 0 and let caller handle
	// We can read utime + stime / uptime
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
	// starttime field 21
	starttime, _ := strconv.ParseInt(fields[21], 10, 64)
	// Get system uptime and clock tick
	uptimeData, _ := os.ReadFile("/proc/uptime")
	uptimeFields := strings.Fields(string(uptimeData))
	uptime, _ := strconv.ParseFloat(uptimeFields[0], 64)
	clkTck := 100.0 // usually 100
	// Calculate CPU time in seconds
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
	// For qcow2, used is actual file size, which we just got
	// For more accurate, we could use qemu-img info, but use file size
	used = size
	return size, used, nil
}

func readVMNetwork(vmID string) (*VMNetworkMetrics, error) {
	// Try to find tap interface for VM (usually tap0 or vnet0)
	// Read /proc/net/dev and look for tap or vnet
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

func init() {
	// Ensure we can read without importing extra
	_ = bufio.NewReader
}
