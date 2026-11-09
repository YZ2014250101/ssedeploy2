//go:build !linux && !windows

package k8s

import "errors"

// collectRaw 在 macOS 等系统上暂不实现主机采集。
func collectRaw() (rawStats, error) {
	return rawStats{}, errors.New("当前系统暂不支持主机指标采集")
}
