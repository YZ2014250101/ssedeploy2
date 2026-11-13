package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/model"
)

// ListMiddlewares 返回指定 Pod 名下的中间件列表（含连接信息与实时状态）。
// 中间件归属 Pod，普通用户只能查看自己 Pod 下的实例。
func (h *Handler) ListMiddlewares(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}
	items, err := h.Middlewares.List(c.Request.Context(), podName)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items})
}

// CreateMiddleware 一键创建中间件（PVC + Secret + Deployment + Service），
// 返回自动生成的密码与集群内访问地址。
func (h *Handler) CreateMiddleware(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}

	var req model.CreateMiddlewareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "参数格式不正确")
		return
	}

	inst, err := h.Middlewares.Create(c.Request.Context(), podName, currentUserID(c), req)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, inst)
}

// DeleteMiddleware 删除中间件并级联回收 Deployment / Service / Secret。
// 查询参数 keepPvc 默认为 true：保留数据盘，避免误删导致数据丢失。
func (h *Handler) DeleteMiddleware(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}

	keepPVC := c.DefaultQuery("keepPvc", "true") != "false"
	if err := h.Middlewares.Delete(c.Request.Context(), podName, c.Param("middleware"), keepPVC); err != nil {
		respondErr(c, err)
		return
	}
	OK(c, nil)
}
