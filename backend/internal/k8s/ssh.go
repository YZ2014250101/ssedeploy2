package k8s

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"ssedeploy/internal/model"
)

// ErrSSHUnavailable 表示 SSH 连接或命令执行失败，接口层映射为 502。
var ErrSSHUnavailable = errors.New("SSH 连接失败")

// RunSSHCommand 使用 Pod 的凭证通过密码认证连接远程服务器并执行命令，
// 返回命令输出（stdout + stderr）。ctx 超时会主动中断会话。
func RunSSHCommand(ctx context.Context, cred *model.SSHCredential, command string) (string, error) {
	if cred == nil || cred.Host == "" {
		return "", fmt.Errorf("%w: 缺少 SSH 连接信息", ErrSSHUnavailable)
	}
	addr := net.JoinHostPort(cred.Host, strconv.Itoa(int(cred.Port)))

	config := &ssh.ClientConfig{
		User: cred.Username,
		Auth: []ssh.AuthMethod{ssh.Password(cred.Password)},
		// 目标地址由平台配置下发、端口固定，这里跳过主机密钥校验。
		// 如需更强安全性，可把服务端公钥落库并改用 ssh.FixedHostKey。
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// 先建立 TCP 连接，这样可以复用 ctx 控制建连超时
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSSHUnavailable, err)
	}

	// 在已建立的 TCP 连接上完成 SSH 握手
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("%w: %v", ErrSSHUnavailable, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSSHUnavailable, err)
	}
	defer session.Close()

	// 命令在独立 goroutine 中执行，主流程等待 ctx 超时以支持中断
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := session.CombinedOutput(command)
		done <- result{out: out, err: err}
	}()

	select {
	case <-ctx.Done():
		// 先尝试发送 KILL 信号，再关闭会话强制中断
		_ = session.Signal(ssh.SIGKILL)
		_ = session.Close()
		return "", fmt.Errorf("%w: 执行超时", ErrSSHUnavailable)
	case r := <-done:
		output := strings.TrimSpace(string(r.out))
		if r.err != nil {
			if output != "" {
				return output, fmt.Errorf("%w: %v: %s", ErrSSHUnavailable, r.err, output)
			}
			return "", fmt.Errorf("%w: %v", ErrSSHUnavailable, r.err)
		}
		return output, nil
	}
}
