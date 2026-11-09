//go:build linux

package k8s

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"ssedeploy/internal/model"
)

// readHostStats 读取宿主机 CPU 负载、内存与根分区磁盘使用情况。
// 仅在 Linux 下可用；后端以容器方式部署时，/proc 反映的是宿主机数据。
func readHostStats() (cpu model.CPUStat, memory model.UsageStat, disk model.UsageStat) {
	cpu = model.CPUStat{Cores: runtime.NumCPU()}

	// CPU 1 分钟平均负载：/proc/loadavg 第一个字段
	if raw, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) > 0 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				cpu.Load1m = v
				cpu.Available = true
			}
		}
	}

	// 内存：MemTotal - MemAvailable，单位 kB
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, available int64
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				total = value * 1024
			case "MemAvailable:":
				available = value * 1024
			}
		}
		if total > 0 {
			memory.TotalBytes = total
			memory.UsedBytes = total - available
			memory.Percent = percentOf(memory.UsedBytes, total)
			memory.Available = true
		}
	}

	// 磁盘：根分区使用率，Bavail 表示非 root 用户可用块数
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		blockSize := int64(stat.Bsize)
		total := int64(stat.Blocks) * blockSize
		used := (int64(stat.Blocks) - int64(stat.Bavail)) * blockSize
		if total > 0 {
			disk.TotalBytes = total
			disk.UsedBytes = used
			disk.Percent = percentOf(used, total)
			disk.Available = true
		}
	}

	return cpu, memory, disk
}
