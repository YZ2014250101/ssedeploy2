package handler

import (
	"github.com/gin-gonic/gin"
)

// AdminStorage 管理员存储大盘：返回 PV / PVC / StorageClass 汇总。
// 路由：GET /api/v1/admin/storage（需认证 + 管理员中间件）
func (h *Handler) AdminStorage(c *gin.Context) {
	data, err := h.StorageSvc.Overview(c.Request.Context())
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, data)
}
