package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Overview 概览页聚合接口：一次返回统计卡、集群信息、模块与 GPU 数据。
func (h *Handler) Overview(c *gin.Context) {
	// 采集涉及多个 API 调用与 nvidia-smi，限制 8 秒避免请求堆积
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()

	data, err := h.OverviewSvc.Get(ctx)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, data)
}
