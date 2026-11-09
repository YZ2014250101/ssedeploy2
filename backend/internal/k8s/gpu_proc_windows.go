//go:build windows

package k8s

// podUIDForPID 在 Windows 下无法读取 /proc，返回空字符串，
// Pod 归属退化为按申请数量分配、显存按 Pod 均分。
func podUIDForPID(pid int) string {
	return ""
}
