//go:build linux

package k8s

import "os"

// readLoadAvgFile 读取 /proc/loadavg 并解析前三列。
// 读取失败时返回 0，不影响接口返回结构。
func readLoadAvgFile() (float64, float64, float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	return parseLoadAvg(string(data))
}
