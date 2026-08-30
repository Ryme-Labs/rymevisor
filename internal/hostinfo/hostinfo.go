package hostinfo

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)


type MemoryInfo struct {
	TotalMB      int64
	UsedMB       int64
	FreeMB       int64
	AvailableMB  int64
	UsagePercent float64
	SwapTotalMB  int64
	SwapUsedMB   int64
}


type DiskInfo struct {
	TotalGB      int64
	UsedGB       int64
	FreeGB       int64
	UsagePercent float64
}


type DiskIOInfo struct {
	ReadBytes  int64
	WriteBytes int64
	ReadIOPS   int64
	WriteIOPS  int64
}


type NetInterface struct {
	Name      string
	RxBytes   int64
	TxBytes   int64
	RxPackets int64
	TxPackets int64
	RxErrors  int64
	TxErrors  int64
}


func ReadCPUCores() (int32, error) {
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


func ReadTotalCPUs() (int32, error) {
	return ReadCPUCores()
}


func ReadCPUStat() (idle, total int64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 5 {
				break
			}
			for i, f := range fields[1:] {
				v, _ := strconv.ParseInt(f, 10, 64)
				total += v
				if i == 3 {
					idle = v
				}
			}
			break
		}
	}
	return idle, total, nil
}



func ReadUsedCPUs() (int32, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
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
			if total == 0 {
				return 0, nil
			}
			totalCPUs, _ := ReadCPUCores()
			usage := float64(used) / float64(total) * float64(totalCPUs)
			return int32(usage + 0.5), nil
		}
	}
	return 0, nil
}



func ReadCPUUsage() (float64, error) {
	idle, total, err := ReadCPUStat()
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	used := total - idle
	return 100 * float64(used) / float64(total), nil
}



func ReadMemory() (totalMB, usedMB int64, err error) {
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
	totalMB = totalKB / 1024
	usedMB = (totalKB - availableKB) / 1024
	return totalMB, usedMB, nil
}


func ReadMemoryInfo() (*MemoryInfo, error) {
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
	m := &MemoryInfo{
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


func ReadDisk(path string) (*DiskInfo, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return nil, err
	}
	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bfree) * int64(stat.Bsize)
	used := total - free
	m := &DiskInfo{
		TotalGB: total / (1024 * 1024 * 1024),
		FreeGB:  free / (1024 * 1024 * 1024),
		UsedGB:  used / (1024 * 1024 * 1024),
	}
	if total > 0 {
		m.UsagePercent = float64(used) / float64(total) * 100
	}
	return m, nil
}


func ReadDiskStats() (DiskIOInfo, error) {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return DiskIOInfo{}, err
	}
	var out DiskIOInfo
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		dev := fields[2]
		if strings.HasPrefix(dev, "loop") || strings.HasPrefix(dev, "ram") {
			continue
		}
		if !strings.HasPrefix(dev, "sd") && !strings.HasPrefix(dev, "vd") && !strings.HasPrefix(dev, "nvme") && !strings.HasPrefix(dev, "mmc") {
			continue
		}
		reads, _ := strconv.ParseInt(fields[3], 10, 64)
		sectorsRead, _ := strconv.ParseInt(fields[5], 10, 64)
		writes, _ := strconv.ParseInt(fields[7], 10, 64)
		sectorsWritten, _ := strconv.ParseInt(fields[9], 10, 64)
		out.ReadIOPS += reads
		out.WriteIOPS += writes
		out.ReadBytes += sectorsRead * 512
		out.WriteBytes += sectorsWritten * 512
	}
	return out, nil
}


func ReadLoadAvg() ([]float64, error) {
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


func ReadLoadProcs() (running, total int, err error) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(string(data))
	if len(fields) < 4 {
		return 0, 0, nil
	}
	parts := strings.Split(fields[3], "/")
	if len(parts) != 2 {
		return 0, 0, nil
	}
	running, _ = strconv.Atoi(parts[0])
	total, _ = strconv.Atoi(parts[1])
	return running, total, nil
}


func ReadUptime() (int64, error) {
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


func ReadNetwork() ([]NetInterface, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	var interfaces []NetInterface
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
		interfaces = append(interfaces, NetInterface{
			Name: iface, RxBytes: rxBytes, TxBytes: txBytes,
			RxPackets: rxPackets, TxPackets: txPackets,
			RxErrors: rxErrors, TxErrors: txErrors,
		})
	}
	return interfaces, nil
}


func ReadNetworkTotals() (rxBytes, txBytes, rxPackets, txPackets int64, err error) {
	ifaces, err := ReadNetwork()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	for _, i := range ifaces {
		rxBytes += i.RxBytes
		txBytes += i.TxBytes
		rxPackets += i.RxPackets
		txPackets += i.TxPackets
	}
	return
}
