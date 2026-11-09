package k8s

import (
	"strconv"
	"strings"

	"ssedeploy/internal/model"
)

// parseLoadAvg 解析 /proc/loadavg 内容的前三个浮点数。
// 文件格式示例：0.52 0.58 0.59 1/823 12345
// 依次对应 1min、5min、15min 的平均活跃任务数（运行中 + D 态不可中断睡眠进程）。
func parseLoadAvg(content string) (float64, float64, float64) {
	fields := strings.Fields(content)
	if len(fields) < 3 {
		return 0, 0, 0
	}
	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)
	return load1, load5, load15
}

// ReadNodeHealth 实时读取一次系统负载，不经过 3 秒采样缓存。
// 非 Linux 平台（本地 Windows 调试）没有 /proc/loadavg，返回 0。
func ReadNodeHealth() model.NodeHealth {
	load1, load5, load15 := readLoadAvgFile()
	return model.NodeHealth{Load1: load1, Load5: load5, Load15: load15}
}
