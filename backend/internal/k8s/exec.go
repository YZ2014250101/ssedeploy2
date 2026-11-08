package k8s

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

// Exec 通过 SPDY 连接到 Pod 内 shell，实现 WebSSH。
// stdin/stdout/stderr 由 WebSocket 适配器提供，sizeQ 用于窗口尺寸同步。
func (s *PodService) Exec(
	ctx context.Context,
	name, shell string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	tty bool,
	sizeQ remotecommand.TerminalSizeQueue,
) error {
	p, err := s.runningPod(ctx, name)
	if err != nil {
		return err
	}
	if shell == "" {
		shell = "/bin/sh"
	}

	// 构造 exec 子资源请求
	req := s.client.Kube.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(s.namespace).
		Name(p.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: logContainerName(p),
			Command:   []string{shell},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       tty,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.client.Config, "POST", req.URL())
	if err != nil {
		return err
	}

	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               tty,
		TerminalSizeQueue: sizeQ,
	})
}
