package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/model"
)

// DeploymentWebhook 接收 Git 平台推送回调，无需登录态，靠 token / 签名校验。
// 支持 Gitee（X-Gitee-Token 或 ?token=）与 GitHub（X-Hub-Signature-256）。
func (h *Handler) DeploymentWebhook(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1<<20))
	if err != nil {
		Fail(c, http.StatusBadRequest, "读取请求体失败")
		return
	}

	token := c.Query("token")
	if token == "" {
		token = c.GetHeader("X-Gitee-Token")
	}
	if token == "" {
		token = c.GetHeader("X-Webhook-Token")
	}

	triggered, err := h.DeploySvc.HandleWebhook(
		c.Request.Context(), c.Param("name"), c.Param("deploy"),
		token, c.GetHeader("X-Hub-Signature-256"), body,
	)
	if err != nil {
		respondErr(c, err)
		return
	}
	if !triggered {
		OK(c, gin.H{"triggered": false, "message": "已忽略：推送分支与部署分支不一致"})
		return
	}
	OK(c, gin.H{"triggered": true, "message": "已在 Pod 内触发重新部署"})
}

// ListDeployments 返回某个 Pod 下的部署实例列表。
func (h *Handler) ListDeployments(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	items, err := h.DeploySvc.List(c.Request.Context(), podName, currentUserID(c), currentIsAdmin(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items, "total": len(items)})
}

// CreateDeployment 在指定 Pod 内创建部署：拉取代码到工作区并执行 deploy.sh。
func (h *Handler) CreateDeployment(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	var req model.CreateDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	item, err := h.DeploySvc.Create(c.Request.Context(), podName, req, currentUserID(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"name": item.Name})
}

// UpdateDeployment 编辑部署配置，运行中的实例会重新执行部署脚本。
func (h *Handler) UpdateDeployment(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	var req model.UpdateDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if err := h.DeploySvc.Update(c.Request.Context(), podName, c.Param("deploy"), req, currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// DeleteDeployment 删除部署实例，并清理 Pod 内的代码与状态目录。
func (h *Handler) DeleteDeployment(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	if err := h.DeploySvc.Delete(c.Request.Context(), podName, c.Param("deploy"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// StartDeployment 在 Pod 内启动部署脚本。
func (h *Handler) StartDeployment(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	if err := h.DeploySvc.Start(c.Request.Context(), podName, c.Param("deploy"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// StopDeployment 停止 Pod 内的部署进程。
func (h *Handler) StopDeployment(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	if err := h.DeploySvc.Stop(c.Request.Context(), podName, c.Param("deploy"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// RestartDeployment 重新拉取代码并执行部署脚本。
func (h *Handler) RestartDeployment(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	if err := h.DeploySvc.Restart(c.Request.Context(), podName, c.Param("deploy"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}
