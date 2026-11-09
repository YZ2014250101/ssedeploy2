// Package handler 实现 HTTP 接口层，只做参数解析、调用 service、返回统一响应。
package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"ssedeploy/internal/config"
	"ssedeploy/internal/k8s"
	"ssedeploy/internal/middleware"
	"ssedeploy/internal/store"
)

// Handler 汇总所有接口依赖。
type Handler struct {
	Pods        *k8s.PodService
	OverviewSvc *k8s.OverviewService
	HostSvc     *k8s.HostService
	GPUSvc      *k8s.GPUService
	ClusterSvc  *k8s.ClusterService
	Store       *store.Store
	Cfg         *config.Config
}

// New 创建接口处理器。
func New(pods *k8s.PodService, overview *k8s.OverviewService, host *k8s.HostService, gpu *k8s.GPUService, cluster *k8s.ClusterService, st *store.Store, cfg *config.Config) *Handler {
	return &Handler{Pods: pods, OverviewSvc: overview, HostSvc: host, GPUSvc: gpu, ClusterSvc: cluster, Store: st, Cfg: cfg}
}

// OK 统一成功响应：{code:0, message:"ok", data:...}
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": data})
}

// Fail 统一失败响应，HTTP 状态码与业务 code 保持一致。
func Fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": status, "message": message, "data": nil})
}

// currentUserID 从鉴权中间件写入的上下文读取用户 id。
func currentUserID(c *gin.Context) int64 {
	v, _ := c.Get(middleware.CtxUserID)
	id, _ := v.(int64)
	return id
}

// respondErr 把内部错误映射为合适的 HTTP 状态码。
func respondErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), apierrors.IsNotFound(err):
		Fail(c, http.StatusNotFound, "资源不存在")
	case apierrors.IsAlreadyExists(err):
		Fail(c, http.StatusConflict, "资源已存在")
	default:
		Fail(c, http.StatusInternalServerError, err.Error())
	}
}
