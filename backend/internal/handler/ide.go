package handler

import (
	"github.com/gin-gonic/gin"
)

// IDEPods 返回当前用户自己的 Pod 列表，供 IDE 工作台的 Pod 下拉选择器使用。
// 路由：GET /api/v1/ide/pods
func (h *Handler) IDEPods(c *gin.Context) {
	items, err := h.Pods.ListOwned(c.Request.Context(), currentUserID(c))
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items})
}
