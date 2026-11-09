//go:build windows

package k8s

import (
	"encoding/binary"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"ssedeploy/internal/model"
)

var (
	// procGetSystemTimes 对应 kernel32!GetSystemTimes，用于计算 CPU 使用率
	procGetSystemTimes = kernel32.NewProc("GetSystemTimes")
	// procGetTickCount64 对应 kernel32!GetTickCount64，用于计算运行时间
	procGetTickCount64 = kernel32.NewProc("GetTickCount64")

	// iphlpapi 提供网卡收发计数
	iphlpapi         = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetIfTable2  = iphlpapi.NewProc("GetIfTable2")
	procFreeMibTable = iphlpapi.NewProc("FreeMibTable")

	// pdh 提供 PhysicalDisk 性能计数器，用于本地开发时的磁盘 IO 采集
	pdh                             = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQueryW               = pdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounterW       = pdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData         = pdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterValue = pdh.NewProc("PdhGetFormattedCounterValue")
	procPdhCloseQuery               = pdh.NewProc("PdhCloseQuery")
)

// pdhCounterValue 对应 PDH_FMT_COUNTERVALUE，只使用 double 分支。
type pdhCounterValue struct {
	CStatus uint32
	_       uint32
	Value   float64
}

// diskCounterSet 用 PDH 的每秒速率计数器合成累计值，
// 供采样器沿用「差值 / 间隔」的统一换算逻辑。
type diskCounterSet struct {
	query     uintptr
	readBps   uintptr
	writeBps  uintptr
	readOps   uintptr
	writeOps  uintptr
	ready     bool
	failed    bool
	lastAt    time.Time
	readBytes uint64
	writeBytes uint64
	reads     uint64
	writes    uint64
}

// winDisk 是全局磁盘计数器，只有主机采样 goroutine 会访问。
var winDisk diskCounterSet

// init 打开 PDH 查询并添加 PhysicalDisk(_Total) 的四个速率计数器。
func (d *diskCounterSet) init() {
	if d.ready || d.failed {
		return
	}
	if r, _, _ := procPdhOpenQueryW.Call(0, 0, uintptr(unsafe.Pointer(&d.query))); r != 0 {
		d.failed = true
		return
	}
	add := func(path string, target *uintptr) bool {
		p, err := windows.UTF16PtrFromString(path)
		if err != nil {
			return false
		}
		r, _, _ := procPdhAddEnglishCounterW.Call(d.query, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(target)))
		return r == 0
	}
	if !add(`\PhysicalDisk(_Total)\Disk Read Bytes/sec`, &d.readBps) ||
		!add(`\PhysicalDisk(_Total)\Disk Write Bytes/sec`, &d.writeBps) ||
		!add(`\PhysicalDisk(_Total)\Disk Reads/sec`, &d.readOps) ||
		!add(`\PhysicalDisk(_Total)\Disk Writes/sec`, &d.writeOps) {
		procPdhCloseQuery.Call(d.query)
		d.failed = true
		return
	}
	// 速率计数器需要两次采样才能给出有效值，这里先预热一次
	procPdhCollectQueryData.Call(d.query)
	d.lastAt = time.Now()
	d.ready = true
}

// sample 读取一次速率并按间隔累加到合成计数器中。
func (d *diskCounterSet) sample(raw *rawStats) {
	d.init()
	if !d.ready {
		return
	}
	if r, _, _ := procPdhCollectQueryData.Call(d.query); r != 0 {
		return
	}
	now := time.Now()
	elapsed := now.Sub(d.lastAt).Seconds()
	if elapsed <= 0 {
		return
	}
	d.lastAt = now

	readBps := pdhValue(d.readBps)
	writeBps := pdhValue(d.writeBps)
	readOps := pdhValue(d.readOps)
	writeOps := pdhValue(d.writeOps)

	d.readBytes += toCount(readBps * elapsed)
	d.writeBytes += toCount(writeBps * elapsed)
	d.reads += toCount(readOps * elapsed)
	d.writes += toCount(writeOps * elapsed)

	raw.diskReadBytes = d.readBytes
	raw.diskWriteBytes = d.writeBytes
	raw.diskReads = d.reads
	raw.diskWrites = d.writes
}

// pdhValue 读取计数器的 double 值，失败返回 0。
func pdhValue(counter uintptr) float64 {
	if counter == 0 {
		return 0
	}
	const pdhFmtDouble = 0x00000200
	var value pdhCounterValue
	r, _, _ := procPdhGetFormattedCounterValue.Call(counter, pdhFmtDouble, 0, uintptr(unsafe.Pointer(&value)))
	if r != 0 || value.CStatus != 0 {
		return 0
	}
	return value.Value
}

// toCount 把速率×时间转换为非负整数计数。
func toCount(value float64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}

// MIB_IF_ROW2 在 x64 下的字段偏移（Windows SDK iphlpapi.h）。
// 只取统计需要的字段，避免在 Go 中完整复刻整张结构体。
const (
	mibIfRow2Size       = 1352 // sizeof(MIB_IF_ROW2)
	mibIfTable2Rows     = 8    // NumEntries(4) + 8 字节对齐填充
	offIfRow2Type       = 1128 // IFTYPE
	offIfRow2OperStatus = 1156 // IF_OPER_STATUS
	offIfRow2InOctets   = 1208 // 接收字节
	offIfRow2InUcast    = 1216 // 接收单播包
	offIfRow2InNucast   = 1224 // 接收非单播包
	offIfRow2InDiscards = 1232 // 接收丢弃
	offIfRow2OutOctets  = 1280 // 发送字节
	offIfRow2OutUcast   = 1288 // 发送单播包
	offIfRow2OutNucast  = 1296 // 发送非单播包
	offIfRow2OutDiscard = 1304 // 发送丢弃

	ifTypeSoftwareLoopback = 24  // IF_TYPE_SOFTWARE_LOOPBACK
	ifTypeTunnel           = 131 // IF_TYPE_TUNNEL
	ifOperStatusUp         = 1   // IF_OPER_STATUS_UP
)

// readNetworkStats 通过 GetIfTable2 汇总所有非回环网卡的收发字节与包计数。
// 计数为 64 位累计值，由采样器做差值换算成速率。
func readNetworkStats(raw *rawStats) {
	var table uintptr
	if r1, _, _ := procGetIfTable2.Call(uintptr(unsafe.Pointer(&table))); r1 != 0 || table == 0 {
		return
	}
	defer procFreeMibTable.Call(table)

	// 前 8 字节是网卡数量，表项紧随其后
	header := unsafe.Slice((*byte)(unsafe.Pointer(table)), mibIfTable2Rows)
	count := int(binary.LittleEndian.Uint32(header))
	if count <= 0 {
		return
	}

	buf := unsafe.Slice((*byte)(unsafe.Pointer(table)), mibIfTable2Rows+count*mibIfRow2Size)
	for i := 0; i < count; i++ {
		row := buf[mibIfTable2Rows+i*mibIfRow2Size:]

		// 排除回环与隧道网卡
		ifType := binary.LittleEndian.Uint32(row[offIfRow2Type:])
		if ifType == ifTypeSoftwareLoopback || ifType == ifTypeTunnel {
			continue
		}
		// 只统计已启用的网卡
		if binary.LittleEndian.Uint32(row[offIfRow2OperStatus:]) != ifOperStatusUp {
			continue
		}

		raw.netRxBytes += binary.LittleEndian.Uint64(row[offIfRow2InOctets:])
		raw.netRxPkts += binary.LittleEndian.Uint64(row[offIfRow2InUcast:]) +
			binary.LittleEndian.Uint64(row[offIfRow2InNucast:])
		raw.netRxDrop += binary.LittleEndian.Uint64(row[offIfRow2InDiscards:])
		raw.netTxBytes += binary.LittleEndian.Uint64(row[offIfRow2OutOctets:])
		raw.netTxPkts += binary.LittleEndian.Uint64(row[offIfRow2OutUcast:]) +
			binary.LittleEndian.Uint64(row[offIfRow2OutNucast:])
		raw.netTxDrop += binary.LittleEndian.Uint64(row[offIfRow2OutDiscard:])
	}
}

// collectRaw 在 Windows 上采集 CPU、内存、运行时间、网卡与系统盘使用情况。
func collectRaw() (rawStats, error) {
	var raw rawStats

	// CPU：内核时间包含空闲时间，换算成与 /proc/stat 一致的时间片
	var idle, kernel, user windows.Filetime
	ret, _, callErr := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ret == 0 {
		return raw, callErr
	}
	raw.cpuIdle = uint64(idle.Nanoseconds() / 100)
	raw.cpuTotal = uint64((kernel.Nanoseconds() + user.Nanoseconds()) / 100)

	// 内存
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	if err := globalMemoryStatusEx(&status); err == nil {
		raw.memTotal = status.TotalPhys
		raw.memAvail = status.AvailPhys
	}

	// 网络：汇总非回环网卡的累计收发计数
	readNetworkStats(&raw)

	// 磁盘 IO：PDH PhysicalDisk 速率计数器合成累计计数
	winDisk.sample(&raw)

	// 运行时间：GetTickCount64 返回毫秒
	if ticks, _, _ := procGetTickCount64.Call(); ticks > 0 {
		raw.uptime = uint64(ticks) / 1000
	}

	// 磁盘：系统盘
	root := os.Getenv("SystemDrive")
	if root == "" {
		root = "C:"
	}
	if path, err := windows.UTF16PtrFromString(root + `\`); err == nil {
		var freeBytes, totalBytes, totalFreeBytes uint64
		if err := windows.GetDiskFreeSpaceEx(path, &freeBytes, &totalBytes, &totalFreeBytes); err == nil && totalBytes > 0 {
			raw.rootTotal = totalBytes
			raw.rootUsed = totalBytes - freeBytes
			raw.partitions = []model.DiskPartition{{
				Mount:      root + `\`,
				UsedBytes:  totalBytes - freeBytes,
				TotalBytes: totalBytes,
				Percent:    clampPercent(float64(totalBytes-freeBytes) / float64(totalBytes) * 100),
			}}
		}
	}

	raw.collected = true
	return raw, nil
}
