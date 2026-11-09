//go:build !linux

package k8s

// readLoadAvgFile 在非 Linux 平台不可用：Windows 没有 /proc/loadavg，
// 本地调试时返回 0，生产环境（容器 / K3s 节点）走 Linux 实现。
func readLoadAvgFile() (float64, float64, float64) {
	return 0, 0, 0
}
