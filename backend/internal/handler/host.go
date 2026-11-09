package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// HostDetail 主机健康详情：实时指标 + 最近 120 个采样点 + 节点/磁盘/GPU 信息。
func (h *Handler) HostDetail(c *gin.Context) {
	// nodeName 传 self 表示后端所在节点
	nodeName := c.Param("nodeName")

	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()

	data, err := h.HostSvc.Get(ctx, nodeName)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, data)
}

// NodeHealth 返回 /proc/loadavg 的 1/5/15 分钟平均负载。
// 注意：平均负载统计的是活跃任务数（含 D 态进程），不等于 CPU 使用率。
func (h *Handler) NodeHealth(c *gin.Context) {
	OK(c, h.HostSvc.NodeHealth())
}
