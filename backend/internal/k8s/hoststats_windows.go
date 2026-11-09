//go:build windows

package k8s

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"

	"ssedeploy/internal/model"
)

// memoryStatusEx 对应 Windows MEMORYSTATUSEX 结构体。
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	// globalMemoryStatusExProc 对应 kernel32!GlobalMemoryStatusEx，x/sys 未封装，这里直接调用
	globalMemoryStatusExProc = kernel32.NewProc("GlobalMemoryStatusEx")
)

// globalMemoryStatusEx 调用系统 API 获取物理内存总量与可用量。
func globalMemoryStatusEx(status *memoryStatusEx) error {
	r1, _, err := globalMemoryStatusExProc.Call(uintptr(unsafe.Pointer(status)))
	if r1 == 0 {
		return err
	}
	return nil
}


// readHostStats 在 Windows 上通过系统 API 读取内存与系统盘使用情况。
// Windows 没有 loadavg 概念，CPU 1 分钟负载标记为不可用。
func readHostStats() (cpu model.CPUStat, memory model.UsageStat, disk model.UsageStat) {
	cpu = model.CPUStat{Cores: runtime.NumCPU()}

	// 内存：GlobalMemoryStatusEx
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	if err := globalMemoryStatusEx(&status); err == nil && status.TotalPhys > 0 {
		memory.TotalBytes = int64(status.TotalPhys)
		memory.UsedBytes = int64(status.TotalPhys - status.AvailPhys)
		memory.Percent = percentOf(memory.UsedBytes, memory.TotalBytes)
		memory.Available = true
	}

	// 磁盘：系统盘（默认 C:\）
	root := os.Getenv("SystemDrive")
	if root == "" {
		root = "C:"
	}
	path, err := windows.UTF16PtrFromString(root + `\`)
	if err != nil {
		return cpu, memory, disk
	}
	var freeBytes, totalBytes, totalFreeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(path, &freeBytes, &totalBytes, &totalFreeBytes); err == nil && totalBytes > 0 {
		disk.TotalBytes = int64(totalBytes)
		disk.UsedBytes = int64(totalBytes - freeBytes)
		disk.Percent = percentOf(disk.UsedBytes, disk.TotalBytes)
		disk.Available = true
	}

	return cpu, memory, disk
}
