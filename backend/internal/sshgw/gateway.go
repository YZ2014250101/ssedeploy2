// Package sshgw 实现平台内置的 SSH 网关。
//
// 网关按每个 Pod 的凭证端口监听，认证通过后把 SSH 会话通过
// Kubernetes exec API 转发进对应 Pod 的容器，因此 Pod 内无需运行 sshd。
package sshgw

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	executil "k8s.io/client-go/util/exec"

	"ssedeploy/internal/k8s"
	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

// mainContainer 与平台创建 Pod 时的主容器名保持一致。
const mainContainer = "main"

// Gateway 是平台内置的 SSH 网关。
// 每个 Pod 拥有独立端口，端口到 Pod 的映射由 pod_ssh_credentials 表维护。
type Gateway struct {
	client    *k8s.Client
	store     *store.Store
	namespace string
	hostKey   ssh.Signer
	ctx       context.Context

	mu        sync.Mutex
	listeners map[int32]net.Listener
}

// New 创建 SSH 网关。主机密钥持久化到 keyPath，
// 避免后端重启后客户端 known_hosts 校验失败；keyPath 为空时退化为临时密钥。
func New(client *k8s.Client, st *store.Store, keyPath string) (*Gateway, error) {
	signer, err := loadOrCreateHostKey(keyPath)
	if err != nil {
		return nil, err
	}
	return &Gateway{
		client:    client,
		store:     st,
		namespace: client.Namespace,
		hostKey:   signer,
		ctx:       context.Background(),
		listeners: make(map[int32]net.Listener),
	}, nil
}

// loadOrCreateHostKey 读取已存在的主机密钥，不存在时生成并写入文件。
func loadOrCreateHostKey(keyPath string) (ssh.Signer, error) {
	if keyPath != "" {
		if raw, err := os.ReadFile(keyPath); err == nil {
			signer, err := ssh.ParsePrivateKey(raw)
			if err == nil {
				return signer, nil
			}
			log.Printf("[ssh-gw] 解析主机密钥失败，将重新生成: %v", err)
		}
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("生成 SSH 主机密钥失败: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, fmt.Errorf("创建 SSH 主机密钥失败: %w", err)
	}

	if keyPath != "" {
		der, err := x509.MarshalPKCS8PrivateKey(priv)
		if err == nil {
			block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
			if err := os.WriteFile(keyPath, block, 0o600); err != nil {
				log.Printf("[ssh-gw] 写入主机密钥失败（重启后密钥将变化）: %v", err)
			}
		}
	}
	return signer, nil
}

// Start 读取所有已签发的凭证，为每个端口恢复监听。
func (g *Gateway) Start(ctx context.Context) {
	g.ctx = ctx
	creds, err := g.store.ListSSHCredentials(g.namespace)
	if err != nil {
		log.Printf("[ssh-gw] 读取 SSH 凭证失败: %v", err)
		return
	}
	for _, c := range creds {
		g.EnablePort(c.Port)
	}
	g.mu.Lock()
	count := len(g.listeners)
	g.mu.Unlock()
	log.Printf("[ssh-gw] SSH 网关已启动，已监听 %d 个 Pod 端口", count)
}

// EnablePort 在指定端口启动监听；重复调用无副作用。
// 返回 true 表示该端口当前可用（已监听或监听成功）。
func (g *Gateway) EnablePort(port int32) bool {
	if port <= 0 {
		return false
	}
	g.mu.Lock()
	if _, ok := g.listeners[port]; ok {
		g.mu.Unlock()
		return true
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		g.mu.Unlock()
		log.Printf("[ssh-gw] 监听端口 %d 失败: %v", port, err)
		return false
	}
	g.listeners[port] = ln
	g.mu.Unlock()

	log.Printf("[ssh-gw] 已监听 SSH 端口 %d", port)
	go g.serve(port, ln)
	return true
}

// DisablePort 关闭端口监听（Pod 删除时调用）。
func (g *Gateway) DisablePort(port int32) {
	g.mu.Lock()
	ln, ok := g.listeners[port]
	delete(g.listeners, port)
	g.mu.Unlock()
	if ok {
		_ = ln.Close()
		log.Printf("[ssh-gw] 已释放 SSH 端口 %d", port)
	}
}

// serve 接受该端口上的连接，每个连接独立处理。
func (g *Gateway) serve(port int32, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go g.handleConn(port, conn)
	}
}

// handleConn 完成 SSH 握手与认证，之后只接受 session 通道。
func (g *Gateway) handleConn(port int32, raw net.Conn) {
	defer raw.Close()

	remoteHost, remotePort := splitRemoteAddr(raw.RemoteAddr())
	remoteAddr := raw.RemoteAddr().String()

	// 认证回调与连接结束可能在不同 goroutine 中读写这些状态
	var (
		stateMu  sync.Mutex
		podName  string
		username string
		authed   bool
	)

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			cred, err := g.store.GetSSHCredentialByPort(g.namespace, port)
			if err != nil {
				return nil, errors.New("凭证不存在")
			}

			stateMu.Lock()
			podName = cred.PodName
			username = meta.User()
			stateMu.Unlock()

			if meta.User() != cred.Username ||
				subtle.ConstantTimeCompare(pass, []byte(cred.Password)) != 1 {
				g.saveEvent(cred.PodName, meta.User(), remoteAddr, model.SSHEventFailed,
					fmt.Sprintf("Failed password for %s from %s port %s ssh2", meta.User(), remoteHost, remotePort))
				return nil, errors.New("用户名或密码错误")
			}

			stateMu.Lock()
			authed = true
			stateMu.Unlock()
			g.saveEvent(cred.PodName, meta.User(), remoteAddr, model.SSHEventAccepted,
				fmt.Sprintf("Accepted password for %s from %s port %s ssh2", meta.User(), remoteHost, remotePort))
			return &ssh.Permissions{Extensions: map[string]string{"pod": cred.PodName}}, nil
		},
	}
	cfg.AddHostKey(g.hostKey)

	sconn, chans, reqs, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		// 握手阶段失败：记录认证前断开，便于排查暴力破解与误连
		stateMu.Lock()
		p, u, ok := podName, username, authed
		stateMu.Unlock()
		if p != "" && u != "" && !ok {
			g.saveEvent(p, u, remoteAddr, model.SSHEventDisconnect,
				fmt.Sprintf("Disconnected from authenticating user %s %s port %s [preauth]", u, remoteHost, remotePort))
		}
		return
	}
	defer sconn.Close()
	// 认证通过后连接关闭时记录断开事件
	defer func() {
		stateMu.Lock()
		p, u := podName, username
		stateMu.Unlock()
		if p != "" {
			g.saveEvent(p, u, remoteAddr, model.SSHEventDisconnect,
				fmt.Sprintf("Received disconnect from %s port %s:11: Bye Bye", remoteHost, remotePort))
		}
	}()
	go ssh.DiscardRequests(reqs)

	stateMu.Lock()
	podName = sconn.Permissions.Extensions["pod"]
	activePod := podName
	stateMu.Unlock()

	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "仅支持 session 通道")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go g.handleSession(activePod, ch, chReqs)
	}
}

// saveEvent 持久化一条 SSH 连接记录；失败只记日志，不影响连接本身。
func (g *Gateway) saveEvent(podName, username, remoteAddr, event, message string) {
	if podName == "" {
		return
	}
	if err := g.store.SaveSSHEvent(&model.SSHEvent{
		Namespace:  g.namespace,
		PodName:    podName,
		Username:   username,
		RemoteAddr: remoteAddr,
		Event:      event,
		Message:    message,
	}); err != nil {
		log.Printf("[ssh-gw] 保存 SSH 连接记录失败: %v", err)
	}
}

// splitRemoteAddr 拆分客户端地址，取不到端口时只返回主机部分。
func splitRemoteAddr(addr net.Addr) (host, port string) {
	if addr == nil {
		return "", ""
	}
	h, p, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String(), ""
	}
	return h, p
}

// handleSession 处理 shell / exec 请求，并支持 PTY 与窗口大小变化。
func (g *Gateway) handleSession(podName string, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer ch.Close()

	var (
		pty     bool
		sizeQ   = newTermSizeQueue()
		started bool
	)
	defer sizeQ.close()

	for req := range reqs {
		switch req.Type {
		case "pty-req":
			var payload struct {
				Term          string
				Columns, Rows uint32
				Width, Height uint32
				Modes         string
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err == nil {
				pty = true
				sizeQ.push(remotecommand.TerminalSize{Width: uint16(payload.Columns), Height: uint16(payload.Rows)})
			}
			_ = req.Reply(true, nil)

		case "window-change":
			var payload struct {
				Columns, Rows uint32
				Width, Height uint32
			}
			if err := ssh.Unmarshal(req.Payload, &payload); err == nil {
				sizeQ.push(remotecommand.TerminalSize{Width: uint16(payload.Columns), Height: uint16(payload.Rows)})
			}
			// window-change 按协议不需要回复

		case "env":
			// 平台不注入客户端环境变量，避免绕过容器安全策略
			_ = req.Reply(false, nil)

		case "shell":
			if started {
				_ = req.Reply(false, nil)
				continue
			}
			started = true
			_ = req.Reply(true, nil)
			go g.runSession(podName, ch, []string{"/bin/sh"}, pty, sizeQ)

		case "exec":
			if started {
				_ = req.Reply(false, nil)
				continue
			}
			started = true
			command := parseExecPayload(req.Payload)
			_ = req.Reply(true, nil)
			go g.runSession(podName, ch, command, pty, sizeQ)

		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

// runSession 把 SSH 会话接到 Kubernetes exec，结束后回传退出码。
func (g *Gateway) runSession(podName string, ch ssh.Channel, command []string, pty bool, sizeQ *termSizeQueue) {
	opts := remotecommand.StreamOptions{
		Stdin:  ch,
		Stdout: ch,
		Tty:    pty,
	}
	if pty {
		opts.TerminalSizeQueue = sizeQ
	} else {
		opts.Stderr = ch.Stderr()
	}

	err := g.execInPod(g.ctx, podName, command, opts)

	status := 0
	if err != nil {
		status = 1
		var exitErr executil.CodeExitError
		if errors.As(err, &exitErr) {
			status = exitErr.Code
		}
	}
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(status)}))
	_ = ch.Close()
}

// execInPod 通过 pods/exec 子资源在目标 Pod 中执行命令。
func (g *Gateway) execInPod(ctx context.Context, podName string, command []string, opts remotecommand.StreamOptions) error {
	pod, err := g.pickPod(ctx, podName)
	if err != nil {
		return err
	}

	container := mainContainer
	found := false
	for _, c := range pod.Spec.Containers {
		if c.Name == mainContainer {
			found = true
			break
		}
	}
	if !found && len(pod.Spec.Containers) > 0 {
		container = pod.Spec.Containers[0].Name
	}

	req := g.client.Kube.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    !opts.Tty,
			TTY:       opts.Tty,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(g.client.Config, "POST", req.URL())
	if err != nil {
		return fmt.Errorf("创建 exec 会话失败: %w", err)
	}
	return executor.StreamWithContext(ctx, opts)
}

// pickPod 按 app 标签找到运行中的 Pod，未运行则回退到第一个匹配项。
func (g *Gateway) pickPod(ctx context.Context, podName string) (*corev1.Pod, error) {
	selector := fmt.Sprintf("app=%s,%s=true", podName, k8s.ManagedLabel)
	pods, err := g.client.Kube.CoreV1().Pods(g.namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("查询 Pod 失败: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("Pod %s 不存在或尚未创建", podName)
	}
	for i := range pods.Items {
		if pods.Items[i].Status.Phase == corev1.PodRunning {
			return &pods.Items[i], nil
		}
	}
	return &pods.Items[0], nil
}

// parseExecPayload 解析 exec 请求中的命令，统一用 /bin/sh -c 执行。
func parseExecPayload(payload []byte) []string {
	var req struct{ Command string }
	if err := ssh.Unmarshal(payload, &req); err != nil || req.Command == "" {
		return []string{"/bin/sh"}
	}
	return []string{"/bin/sh", "-c", req.Command}
}

// termSizeQueue 实现 remotecommand.TerminalSizeQueue，
// 把 SSH window-change 事件转换为容器 PTY 的尺寸变化。
type termSizeQueue struct {
	ch   chan remotecommand.TerminalSize
	done chan struct{}
}

func newTermSizeQueue() *termSizeQueue {
	return &termSizeQueue{
		ch:   make(chan remotecommand.TerminalSize, 8),
		done: make(chan struct{}),
	}
}

func (q *termSizeQueue) push(size remotecommand.TerminalSize) {
	select {
	case q.ch <- size:
	case <-q.done:
	default:
	}
}

// Next 由 exec 的 resize 协程持续调用，会话结束后返回 nil 以退出。
func (q *termSizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case size := <-q.ch:
		return &size
	case <-q.done:
		return nil
	}
}

func (q *termSizeQueue) close() {
	close(q.done)
}
