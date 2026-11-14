package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/k8s"
)

// GetPodCredentials 返回指定 Pod 的 SSH 连接凭证。
// 创建 Pod 时已自动签发；历史 Pod 首次访问时按当前配置补发。
func (h *Handler) GetPodCredentials(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}

	cred, err := h.Pods.EnsureSSHCredential(c.Request.Context(), podName)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, cred)
}

// TestPodSSH 使用 Pod 的凭证实际连接 SSH 网关并执行命令，用于验证连通性。
// 返回命令输出（用户名与主机名），便于用户确认登录到了正确的环境。
func (h *Handler) TestPodSSH(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}

	cred, err := h.Pods.EnsureSSHCredential(c.Request.Context(), podName)
	if err != nil {
		respondErr(c, err)
		return
	}

	// 单次探测最多等待 20 秒，避免网关不可达时请求长时间挂起
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()

	// 内置 SSH 网关与后端同进程，探测固定走本机回环，
	// 不受凭证对外展示的 host（可能尚未配置域名）影响。
	probe := *cred
	probe.Host = "127.0.0.1"

	output, err := k8s.RunSSHCommand(ctx, &probe, "whoami && hostname")
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"output": output})
}
