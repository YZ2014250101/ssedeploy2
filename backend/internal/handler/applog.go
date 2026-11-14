package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// AppLogFiles 列举工作区 logs 目录下的日志文件（名称 + 大小）。
// 路由：GET /api/v1/pods/:name/app-logs/files
func (h *Handler) AppLogFiles(c *gin.Context) {
	name := c.Param("name")
	if err := h.ensurePodAccess(c, name); err != nil {
		respondErr(c, err)
		return
	}

	items, err := h.Pods.AppLogFiles(c.Request.Context(), name)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, gin.H{"items": items})
}

// AppLog 读取日志文件末尾 N 行。
// 路由：GET /api/v1/pods/:name/app-logs?fileName=app.log&lines=500
func (h *Handler) AppLog(c *gin.Context) {
	name := c.Param("name")
	if err := h.ensurePodAccess(c, name); err != nil {
		respondErr(c, err)
		return
	}

	// 行数非法时交给服务层使用默认值（500 行）
	lines, _ := strconv.Atoi(c.DefaultQuery("lines", "500"))
	result, err := h.Pods.AppLog(c.Request.Context(), name, c.Query("fileName"), lines)
	if err != nil {
		respondErr(c, err)
		return
	}
	OK(c, result)
}
