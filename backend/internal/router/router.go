// Package router 负责路由注册与跨域配置。
package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"ssedeploy/internal/config"
	"ssedeploy/internal/handler"
	"ssedeploy/internal/middleware"
)

// cors 允许本地 Vite 开发服（不同端口）直接调用接口。
func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// New 构建 Gin 引擎并注册全部接口。
func New(cfg *config.Config, h *handler.Handler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), cors())

	// 健康检查，供 docker-compose / K8s 探针使用
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := r.Group("/api/v1")
	api.POST("/auth/login", h.Login)

	auth := api.Group("")
	auth.Use(middleware.Auth(cfg.JWTSecret))
	{
		auth.GET("/auth/me", h.Me)

		// Pod 管理
		auth.GET("/pods", h.ListPods)
		auth.GET("/pods/stats", h.PodStats)
		auth.POST("/pods", h.CreatePod)
		auth.GET("/pods/:name", h.GetPod)
		auth.PUT("/pods/:name", h.UpdatePod)
		auth.DELETE("/pods/:name", h.DeletePod)
		auth.POST("/pods/:name/start", h.StartPod)
		auth.POST("/pods/:name/stop", h.StopPod)
		auth.POST("/pods/:name/restart", h.RestartPod)

		// 监控与事件
		auth.GET("/pods/:name/metrics", h.Metrics)
		auth.GET("/pods/:name/events", h.Events)

		// 日志（HTTP 拉取 + WebSocket 实时）
		auth.GET("/pods/:name/logs", h.Logs)
		auth.GET("/pods/:name/logs/stream", h.StreamLogs)

		// WebSSH
		auth.GET("/pods/:name/terminal", h.Terminal)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "接口不存在", "data": nil})
	})
	return r
}
