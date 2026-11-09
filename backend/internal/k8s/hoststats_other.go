//go:build !linux && !windows

package k8s

import (
	"runtime"

	"ssedeploy/internal/model"
)

// readHostStats 在 macOS 等系统上暂不做宿主机采集，
// 返回 available=false，由前端显示为「--」。
func readHostStats() (cpu model.CPUStat, memory model.UsageStat, disk model.UsageStat) {
	return model.CPUStat{Cores: runtime.NumCPU()}, model.UsageStat{}, model.UsageStat{}
}
