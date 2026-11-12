package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Logs 一次性拉取最近的日志文本（打开日志 Tab 时使用）。
func (h *Handler) Logs(c *gin.Context) {
	// 权限校验：只能拉取自己创建的 Pod 日志
	if err := h.ensurePodAccess(c, c.Param("name")); err != nil {
		respondErr(c, err)
		return
	}

	tail, _ := strconv.ParseInt(c.DefaultQuery("tailLines", "500"), 10, 64)
	content, err := h.Pods.Log(c.Request.Context(), c.Param("name"), tail)
	if err != nil {
		Fail(c, http.StatusBadRequest, err.Error())
		return
	}
	OK(c, gin.H{"content": content})
}
