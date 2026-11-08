package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/model"
)

// ListPods 返回 Pod 列表，支持关键字与状态过滤。
func (h *Handler) ListPods(c *gin.Context) {
	views, err := h.Pods.List(c.Request.Context(), c.Query("keyword"), c.Query("status"), currentUserID(c))
	if err != nil {
		Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	OK(c, gin.H{"items": views, "total": len(views)})
}

// PodStats 返回运行 / 已停止 / 失败 / 我的 四个统计数字。
func (h *Handler) PodStats(c *gin.Context) {
	stats, err := h.Pods.Stats(c.Request.Context(), currentUserID(c))
	if err != nil {
		Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	OK(c, stats)
}

// GetPod 返回 Pod 详情。
func (h *Handler) GetPod(c *gin.Context) {
	detail, err := h.Pods.Get(c.Request.Context(), c.Param("name"), currentUserID(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, detail)
}

// CreatePod 创建 Pod（底层为 Deployment + PVC）。
func (h *Handler) CreatePod(c *gin.Context) {
	var req model.CreatePodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "请求参数格式错误")
		return
	}
	if err := h.Pods.Create(c.Request.Context(), req, currentUserID(c)); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"name": req.Name})
}

// UpdatePod 更新镜像、规格、环境变量与启动命令。
func (h *Handler) UpdatePod(c *gin.Context) {
	var req model.UpdatePodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "请求参数格式错误")
		return
	}
	if err := h.Pods.Update(c.Request.Context(), c.Param("name"), req); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// DeletePod 删除 Pod 及其存储。
func (h *Handler) DeletePod(c *gin.Context) {
	if err := h.Pods.Delete(c.Request.Context(), c.Param("name")); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// StartPod 启动 Pod：副本数调整为 1。
func (h *Handler) StartPod(c *gin.Context) {
	if err := h.Pods.Scale(c.Request.Context(), c.Param("name"), 1); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// StopPod 停止 Pod：副本数调整为 0。
func (h *Handler) StopPod(c *gin.Context) {
	if err := h.Pods.Scale(c.Request.Context(), c.Param("name"), 0); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}

// RestartPod 滚动重启 Pod。
func (h *Handler) RestartPod(c *gin.Context) {
	if err := h.Pods.Restart(c.Request.Context(), c.Param("name")); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}
