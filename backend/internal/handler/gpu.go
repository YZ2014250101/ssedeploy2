package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// GPUOverview GPU 监控页：汇总信息 + 每卡指标 + 时序趋势 + Pod 归属。
func (h *Handler) GPUOverview(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()

	data, err := h.GPUSvc.Get(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, data)
}
