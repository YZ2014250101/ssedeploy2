//go:build linux

package k8s

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"ssedeploy/internal/model"
)

// physicalDiskRe 只匹配物理整盘，排除 loop/ram/dm 等虚拟设备与分区。
var physicalDiskRe = regexp.MustCompile(`^(sd[a-z]+|vd[a-z]+|xvd[a-z]+|hd[a-z]+|nvme\d+n\d+|mmcblk\d+)$`)

// isPhysicalDisk 判断 /proc/diskstats 中的设备名是否为物理整盘。
// loop0、ram0、dm-0、md0 以及 sda1 这类分区都会被排除，避免重复统计。
func isPhysicalDisk(name string) bool {
	return physicalDiskRe.MatchString(name)
}

// virtualNICPrefixes 是虚拟/本地网卡前缀，统计网络吞吐时排除。
// 容器内的网卡通常命名为 eth0，不会被这里排除。
var virtualNICPrefixes = []string{
	"lo", "veth", "docker", "br-", "virbr", "vnet", "tun", "tap",
	"cni", "flannel", "cali", "kube-ipvs0", "nodelocaldns",
}

// isPhysicalNIC 判断网卡名是否为需要统计的物理网卡（排除 lo 与常见虚拟网卡）。
func isPhysicalNIC(name string) bool {
	for _, prefix := range virtualNICPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

// realFsTypes 是磁盘使用卡片中展示的文件系统类型。
var realFsTypes = map[string]bool{
	"ext4": true, "ext3": true, "ext2": true, "xfs": true,
	"btrfs": true, "zfs": true, "vfat": true, "ntfs": true, "f2fs": true,
}

// collectRaw 读取 /proc 文件系统，得到一次采样的累计计数器。
func collectRaw() (rawStats, error) {
	var raw rawStats

	// /proc/stat 是核心数据源，读取失败说明不是 Linux 环境
	statData, err := os.ReadFile("/proc/stat")
	if err != nil {
		return raw, err
	}
	readCPUStat(&raw, string(statData))

	// 其余数据源读取失败时保持零值，不影响整体返回
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		readMeminfo(&raw, string(data))
	}
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		readLoadavg(&raw, string(data))
	}
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		readUptime(&raw, string(data))
	}
	if data, err := os.ReadFile("/proc/net/dev"); err == nil {
		readNetDev(&raw, string(data))
	}
	if data, err := os.ReadFile("/proc/diskstats"); err == nil {
		readDiskStats(&raw, string(data))
	}
	if data, err := os.ReadFile("/proc/mounts"); err == nil {
		readPartitions(&raw, string(data))
	} else {
		readRootUsage(&raw)
	}

	raw.collected = true
	return raw, nil
}

// readCPUStat 解析 /proc/stat 第一行，得到总时间片与空闲时间片。
func readCPUStat(raw *rawStats, content string) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		var total, idle uint64
		for i, field := range fields {
			v, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				continue
			}
			total += v
			// 第 3 个字段是 idle，第 4 个字段是 iowait，空闲时都算不忙
			if i == 3 || i == 4 {
				idle += v
			}
		}
		raw.cpuTotal = total
		raw.cpuIdle = idle
		return
	}
}

// readMeminfo 解析内存与 Swap 信息（单位 kB）。
func readMeminfo(raw *rawStats, content string) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			raw.memTotal = value * 1024
		case "MemAvailable:":
			raw.memAvail = value * 1024
		case "SwapTotal:":
			raw.swapTotal = value * 1024
		case "SwapFree:":
			raw.swapFree = value * 1024
		}
	}
}

// readLoadavg 解析 1/5/15 分钟负载。
func readLoadavg(raw *rawStats, content string) {
	raw.load1, raw.load5, raw.load15 = parseLoadAvg(content)
}

// readUptime 解析主机运行时间。
func readUptime(raw *rawStats, content string) {
	fields := strings.Fields(content)
	if len(fields) == 0 {
		return
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err == nil {
		raw.uptime = uint64(seconds)
	}
}

// readNetDev 汇总所有物理网卡（排除 lo 与虚拟网卡）的收发包与字节数。
func readNetDev(raw *rawStats, content string) {
	lines := strings.Split(content, "\n")
	// 前两行是表头
	for i := 2; i < len(lines); i++ {
		parts := strings.SplitN(lines[i], ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		if !isPhysicalNIC(name) {
			continue
		}
		fields := strings.Fields(parts[1])
		// 字段顺序：rx_bytes rx_packets rx_errs rx_drop ... tx_bytes tx_packets tx_errs tx_drop
		if len(fields) < 16 {
			continue
		}
		raw.netRxBytes += parseUint(fields[0])
		raw.netRxPkts += parseUint(fields[1])
		raw.netRxDrop += parseUint(fields[3])
		raw.netTxBytes += parseUint(fields[8])
		raw.netTxPkts += parseUint(fields[9])
		raw.netTxDrop += parseUint(fields[11])
	}
}

// readDiskStats 汇总整块磁盘的读写次数、扇区与耗时。
func readDiskStats(raw *rawStats, content string) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		if !isPhysicalDisk(fields[2]) {
			continue
		}
		// 第 3 位读次数，第 5 位读扇区，第 6 位读耗时(ms)
		// 第 7 位写次数，第 9 位写扇区，第 10 位写耗时(ms)，第 12 位 IO 总耗时(ms)
		raw.diskReads += parseUint(fields[3])
		raw.diskReadBytes += parseUint(fields[5]) * 512
		raw.diskReadTicks += parseUint(fields[6])
		raw.diskWrites += parseUint(fields[7])
		raw.diskWriteBytes += parseUint(fields[9]) * 512
		raw.diskWritTicks += parseUint(fields[10])
		raw.diskIoTicks += parseUint(fields[12])
	}
}

// readRootUsage 在无法解析挂载表时，至少提供根分区使用率。
func readRootUsage(raw *rawStats) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return
	}
	total := uint64(stat.Blocks) * uint64(stat.Bsize)
	used := (uint64(stat.Blocks) - uint64(stat.Bavail)) * uint64(stat.Bsize)
	raw.rootTotal = total
	raw.rootUsed = used
}

// readPartitions 解析挂载表，返回真实文件系统的使用情况。
func readPartitions(raw *rawStats, content string) {
	parts := make([]model.DiskPartition, 0, 8)
	seen := make(map[string]bool)

	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device, mount, fsType := fields[0], fields[1], fields[2]
		// 只统计真实块设备，跳过 tmpfs / overlay / proc 等
		if !strings.HasPrefix(device, "/dev/") || !realFsTypes[fsType] || seen[mount] {
			continue
		}
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mount, &stat); err != nil {
			continue
		}
		total := uint64(stat.Blocks) * uint64(stat.Bsize)
		if total == 0 {
			continue
		}
		used := (uint64(stat.Blocks) - uint64(stat.Bavail)) * uint64(stat.Bsize)
		seen[mount] = true
		parts = append(parts, model.DiskPartition{
			Mount:      mount,
			UsedBytes:  used,
			TotalBytes: total,
			Percent:    clampPercent(float64(used) / float64(total) * 100),
		})
		// 根分区数据同时用于顶部的磁盘指标卡
		if mount == "/" {
			raw.rootUsed = used
			raw.rootTotal = total
		}
		// 最多展示 10 个分区，避免页面过长
		if len(parts) >= 10 {
			break
		}
	}

	// 挂载表里没有根分区时兜底读一次 statfs
	if raw.rootTotal == 0 {
		readRootUsage(raw)
	}
	raw.partitions = parts
}

// parseUint 解析无符号整数，失败返回 0。
func parseUint(value string) uint64 {
	v, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
