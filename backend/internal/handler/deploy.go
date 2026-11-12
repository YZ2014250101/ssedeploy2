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
		c.Request.Context(), c.Param("name"), token, c.GetHeader("X-Hub-Signature-256"), body,
	)
	if err != nil {
		respondErr(c, err)
		return
	}
	if !triggered {
		OK(c, gin.H{"triggered": false, "message": "已忽略：推送分支与部署分支不一致"})
		return
	}
	OK(c, gin.H{"triggered": true, "message": "已触发重新部署"})
}

// ListDeployments 返回部署实例列表：管理员可见全部，普通用户仅本人创建。
func (h *Handler) ListDeployments(c *gin.Context) {
	items, err := h.DeploySvc.List(c.Request.Context(), currentUserID(c), currentIsAdmin(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items, "total": len(items)})
}

// CreateDeployment 创建部署实例：记录配置并在集群中拉起工作负载。
func (h *Handler) CreateDeployment(c *gin.Context) {
	var req model.CreateDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	item, err := h.DeploySvc.Create(c.Request.Context(), req, currentUserID(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"name": item.Name})
}

// UpdateDeployment 编辑部署配置，工作负载按新配置重建。
func (h *Handler) UpdateDeployment(c *gin.Context) {
	var req model.UpdateDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "请求参数格式错误")
		return
	}
	if err := h.DeploySvc.Update(c.Request.Context(), c.Param("name"), req, currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// DeleteDeployment 删除部署实例及其工作负载。
func (h *Handler) DeleteDeployment(c *gin.Context) {
	if err := h.DeploySvc.Delete(c.Request.Context(), c.Param("name"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// StartDeployment 启动部署实例。
func (h *Handler) StartDeployment(c *gin.Context) {
	if err := h.DeploySvc.Start(c.Request.Context(), c.Param("name"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// StopDeployment 停止部署实例。
func (h *Handler) StopDeployment(c *gin.Context) {
	if err := h.DeploySvc.Stop(c.Request.Context(), c.Param("name"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// RestartDeployment 重启部署实例。
func (h *Handler) RestartDeployment(c *gin.Context) {
	if err := h.DeploySvc.Restart(c.Request.Context(), c.Param("name"), currentUserID(c), currentIsAdmin(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}
