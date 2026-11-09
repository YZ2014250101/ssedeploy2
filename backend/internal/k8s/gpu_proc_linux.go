//go:build linux

package k8s

import (
	"os"
	"regexp"
	"strconv"
)

// podUIDRe 从 cgroup 路径中提取 Pod UID（兼容 cgroup v1/v2 两种命名）。
var podUIDRe = regexp.MustCompile(`pod([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// podUIDForPID 通过 /proc/<pid>/cgroup 反查进程所属的 Pod UID。
func podUIDForPID(pid int) string {
	if pid <= 0 {
		return ""
	}
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
	if err != nil {
		return ""
	}
	match := podUIDRe.FindStringSubmatch(string(data))
	if len(match) < 2 {
		return ""
	}
	return match[1]
}
