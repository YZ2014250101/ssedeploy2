package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/model"
)

// Metrics 返回 Pod 实时的 CPU/内存用量、上限以及趋势序列。
// metrics-server 不可用时 available=false，前端展示空态而不是报错。
func (h *Handler) Metrics(c *gin.Context) {
	name := c.Param("name")

	// 默认展示最近 30 分钟，最大 24 小时
	minutes, _ := strconv.Atoi(c.DefaultQuery("minutes", "30"))
	if minutes <= 0 || minutes > 24*60 {
		minutes = 30
	}
	since := time.Now().Add(-time.Duration(minutes) * time.Minute).Unix()

	cpuMilli, memBytes, usageErr := h.Pods.PodUsage(c.Request.Context(), name)
	cpuLimit, memLimit, _ := h.Pods.Limits(c.Request.Context(), name)
	series, _ := h.Store.QueryMetrics(h.Pods.Namespace(), name, since)
	if series == nil {
		series = []model.MetricPoint{}
	}

	OK(c, gin.H{
		"available":    usageErr == nil,
		"cpuMilli":     cpuMilli,
		"memBytes":     memBytes,
		"cpuLimitMilli": cpuLimit,
		"memLimitBytes": memLimit,
		"series":       series,
	})
}

// Events 返回 Pod 最近事件列表。
func (h *Handler) Events(c *gin.Context) {
	events, err := h.Pods.Events(c.Request.Context(), c.Param("name"))
	if err != nil {
		Fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	OK(c, gin.H{"items": events})
}
