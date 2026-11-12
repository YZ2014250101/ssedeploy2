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

	// 账号认证：统一挂在 /api/v1 分组下（无需登录态）
	api := r.Group("/api/v1")
	api.POST("/login", h.Login)       // 登录
	api.POST("/register", h.Register) // 注册
	api.POST("/auth/login", h.Login)  // 兼容旧路径，逻辑与 /login 完全一致

	auth := api.Group("")
	auth.Use(middleware.Auth(cfg.JWTSecret))
	// 角色以数据库为准，供接口做资源级权限判断（管理员 / 普通用户）
	auth.Use(middleware.ResolveRole(h.Store))
	{
		auth.GET("/auth/me", h.Me)

		// 概览页聚合数据
		auth.GET("/overview", h.Overview)

		// 主机健康详情（nodeName 传 self 表示后端所在节点）
		auth.GET("/host/:nodeName", h.HostDetail)

		// 节点平均负载（/proc/loadavg）
		auth.GET("/node/health", h.NodeHealth)

		// 个人管理页账户信息
		auth.GET("/user/profile", h.UserProfile)

		// GPU 监控页聚合数据
		auth.GET("/gpu", h.GPUOverview)

		// Pod 管理
		auth.GET("/pods", h.ListPods)
		auth.GET("/pods/stats", h.PodStats)

		// IDE 工作台：当前用户自己的 Pod 列表
		auth.GET("/ide/pods", h.IDEPods)
		// IDE 工作区文件：与终端共用同一个 PVC（/home/workspace）
		auth.GET("/ide/pods/:name/files", h.IDEFiles)
		auth.POST("/ide/pods/:name/files", h.IDECreateEntry) // 新建文件/文件夹
		auth.GET("/ide/pods/:name/file", h.IDEFile)
		auth.PUT("/ide/pods/:name/file", h.IDEFileSave)
		auth.PATCH("/ide/pods/:name/file", h.IDERenameEntry)  // 重命名
		auth.DELETE("/ide/pods/:name/file", h.IDEDeleteEntry) // 删除
		auth.GET("/ide/pods/:name/download", h.IDEDownload)   // 下载文件流
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

		// 全局共享目录（/shared）：所有登录用户可见、可读写
		shared := auth.Group("/shared")
		{
			shared.GET("/list", h.SharedList)          // 目录列表
			shared.POST("/mkdir", h.SharedMkdir)       // 新建目录
			shared.POST("/upload", h.SharedUpload)     // 上传文件（多选）
			shared.DELETE("/rm", h.SharedRemove)       // 删除文件/目录
			shared.PATCH("/rename", h.SharedRename)    // 重命名
			shared.GET("/download", h.SharedDownload)  // 下载（目录自动打包 zip）
			shared.GET("/ai/insight", h.SharedInsight) // AI 洞察摘要
		}

		// 管理员专属接口：存储大盘
		admin := auth.Group("/admin")
		admin.Use(middleware.RequireAdmin(h.Store))
		{
			admin.GET("/storage", h.AdminStorage)
		}
	}

	// 概览接口同时保留 /api/overview 别名，兼容对外文档中给出的路径
	alias := r.Group("/api")
	alias.Use(middleware.Auth(cfg.JWTSecret))
	alias.GET("/overview", h.Overview)
	alias.GET("/host/:nodeName", h.HostDetail)
	alias.GET("/node/health", h.NodeHealth)
	alias.GET("/user/profile", h.UserProfile)
	alias.GET("/gpu", h.GPUOverview)

	// 集群页接口：使用独立统一响应结构 { code, msg, data }
	cluster := r.Group("/api/cluster")
	cluster.Use(middleware.Auth(cfg.JWTSecret))
	{
		cluster.GET("/nodes", h.ClusterNodes)
		cluster.GET("/metrics", h.ClusterMetrics)
		cluster.GET("/node/:nodeName/gpus", h.ClusterGPUs)
		cluster.GET("/node/:nodeName/gpu-metrics", h.ClusterGPUMetrics)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "接口不存在", "data": nil})
	})
	return r
}
