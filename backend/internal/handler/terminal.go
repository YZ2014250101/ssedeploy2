package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"k8s.io/client-go/tools/remotecommand"
)

// wsUpgrader 允许跨域握手：鉴权已在中间件中通过 token 完成。
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  8192,
	WriteBufferSize: 8192,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// wsWriter 把 io.Writer 适配到 WebSocket 文本帧，保证并发写安全。
type wsWriter struct {
	mu sync.Mutex
	ws *websocket.Conn
}

func (w *wsWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ws.WriteMessage(websocket.TextMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

// termMessage 终端双向通信协议。
type termMessage struct {
	Type string `json:"type"` // input / resize
	Data string `json:"data"` // 键盘输入
	Cols uint16 `json:"cols"` // 终端列数
	Rows uint16 `json:"rows"` // 终端行数
}

// sizeQueue 把前端窗口尺寸变化传给 SPDY 流。
type sizeQueue struct {
	ch chan remotecommand.TerminalSize
}

func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.ch
	if !ok {
		return nil
	}
	return &size
}

// Terminal 建立 WebSSH：WebSocket <-> SPDY exec。
func (h *Handler) Terminal(c *gin.Context) {
	// 权限校验：只能进入自己创建的 Pod 终端
	if err := h.ensurePodAccess(c, c.Param("name")); err != nil {
		respondErr(c, err)
		return
	}

	shell := c.DefaultQuery("shell", "/bin/sh")

	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrade 已自动写入错误响应
	}
	defer ws.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	sizes := &sizeQueue{ch: make(chan remotecommand.TerminalSize, 8)}
	writer := &wsWriter{ws: ws}

	// 读协程：接收前端输入与窗口尺寸；连接断开时取消 exec
	go func() {
		defer cancel()
		for {
			_, raw, readErr := ws.ReadMessage()
			if readErr != nil {
				return
			}
			var msg termMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			switch msg.Type {
			case "input":
				_, _ = stdinWriter.Write([]byte(msg.Data))
			case "resize":
				if msg.Cols == 0 || msg.Rows == 0 {
					continue
				}
				select {
				case sizes.ch <- remotecommand.TerminalSize{Width: msg.Cols, Height: msg.Rows}:
				default: // 尺寸变化过快时丢弃旧值，避免阻塞
				}
			}
		}
	}()

	if err := h.Pods.Exec(ctx, c.Param("name"), shell, stdinReader, writer, writer, true, sizes); err != nil {
		_, _ = writer.Write([]byte("\r\n\u001b[31m[连接已结束] " + err.Error() + "\u001b[0m\r\n"))
	}
}

// StreamLogs 建立实时日志 WebSocket：服务端持续推送日志文本。
func (h *Handler) StreamLogs(c *gin.Context) {
	// 权限校验：只能查看自己创建的 Pod 日志
	if err := h.ensurePodAccess(c, c.Param("name")); err != nil {
		respondErr(c, err)
		return
	}

	ws, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 读协程仅用于感知前端断开（日志流是单向下行）
	go func() {
		defer cancel()
		for {
			if _, _, readErr := ws.ReadMessage(); readErr != nil {
				return
			}
		}
	}()

	writer := &wsWriter{ws: ws}
	if err := h.Pods.FollowLogs(ctx, c.Param("name"), 200, writer); err != nil {
		_, _ = writer.Write([]byte("\n[日志流结束] " + err.Error() + "\n"))
	}
}
