package k8s

import (
	"context"
	"errors"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
)

// logContainerName 返回适合读日志/进终端的容器名，优先主容器。
func logContainerName(p *corev1.Pod) string {
	for _, c := range p.Spec.Containers {
		if c.Name == mainContainer {
			return c.Name
		}
	}
	if len(p.Spec.Containers) > 0 {
		return p.Spec.Containers[0].Name
	}
	return mainContainer
}

// Log 一次性拉取最近 N 行日志。
func (s *PodService) Log(ctx context.Context, name string, tailLines int64) (string, error) {
	p, err := s.runningPod(ctx, name)
	if err != nil {
		return "", err
	}
	if tailLines <= 0 {
		tailLines = 500
	}

	raw, err := s.client.Kube.CoreV1().Pods(s.namespace).
		GetLogs(p.Name, &corev1.PodLogOptions{
			Container: logContainerName(p),
			TailLines: &tailLines,
		}).DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("读取日志失败: %w", err)
	}
	return string(raw), nil
}

// FollowLogs 实时跟随日志，按块写入 w（用于 WebSocket 推送）。
// 当客户端断开或 Pod 结束时返回。
func (s *PodService) FollowLogs(ctx context.Context, name string, tailLines int64, w io.Writer) error {
	p, err := s.runningPod(ctx, name)
	if err != nil {
		return err
	}
	if tailLines <= 0 {
		tailLines = 200
	}

	stream, err := s.client.Kube.CoreV1().Pods(s.namespace).
		GetLogs(p.Name, &corev1.PodLogOptions{
			Container: logContainerName(p),
			Follow:    true,
			TailLines: &tailLines,
		}).Stream(ctx)
	if err != nil {
		return fmt.Errorf("打开日志流失败: %w", err)
	}
	defer stream.Close()

	buf := make([]byte, 4096)
	for {
		n, readErr := stream.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr // 前端已断开
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}
