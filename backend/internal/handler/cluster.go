package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Resp 是集群页接口的统一响应结构（按接口规范使用 msg 字段）。
type Resp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data"`
}

// clusterOK 返回成功响应。
func clusterOK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Resp{Code: 0, Msg: "ok", Data: data})
}

// clusterFail 返回失败响应，HTTP 状态码与业务 code 一致。
func clusterFail(c *gin.Context, status int, message string) {
	c.JSON(status, Resp{Code: status, Msg: message, Data: nil})
}

// ClusterNodes 获取集群节点列表。
// GET /api/cluster/nodes
func (h *Handler) ClusterNodes(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	defer cancel()

	data, err := h.ClusterSvc.GetNodes(ctx)
	if err != nil {
		clusterFail(c, http.StatusInternalServerError, err.Error())
		return
	}
	clusterOK(c, data)
}

// ClusterMetrics 获取集群全局 CPU / 内存 / 负载时序。
// GET /api/cluster/metrics?range=15m|1h|6h|24h|7d
func (h *Handler) ClusterMetrics(c *gin.Context) {
	clusterOK(c, h.ClusterSvc.GetMetrics(c.Request.Context(), c.Query("range")))
}

// ClusterGPUs 获取单个节点的 GPU 列表。
// GET /api/cluster/node/:nodeName/gpus
func (h *Handler) ClusterGPUs(c *gin.Context) {
	clusterOK(c, h.ClusterSvc.GetGPUs(c.Request.Context(), c.Param("nodeName")))
}

// ClusterGPUMetrics 获取节点 GPU 时序数据。
// GET /api/cluster/node/:nodeName/gpu-metrics?range=15m|1h|6h|24h|7d
func (h *Handler) ClusterGPUMetrics(c *gin.Context) {
	clusterOK(c, h.ClusterSvc.GetGPUMetrics(c.Request.Context(), c.Param("nodeName"), c.Query("range")))
}
