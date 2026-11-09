//go:build !linux && !windows

package k8s

// podUIDForPID 在非 Linux 平台无法读取 /proc，返回空字符串。
func podUIDForPID(pid int) string {
	return ""
}
