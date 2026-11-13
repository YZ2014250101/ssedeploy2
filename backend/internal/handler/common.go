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
	"ssedeploy/internal/service"
	"ssedeploy/internal/shared"
	"ssedeploy/internal/store"
)

// Handler 汇总所有接口依赖。
type Handler struct {
	Pods        *k8s.PodService
	DeploySvc   *k8s.DeployService
	Middlewares *k8s.MiddlewareService
	OverviewSvc *k8s.OverviewService
	HostSvc     *k8s.HostService
	GPUSvc      *k8s.GPUService
	ClusterSvc  *k8s.ClusterService
	StorageSvc  *k8s.StorageService
	SharedSvc   *shared.Service
	AuthSvc     *service.AuthService
	Store       *store.Store
	Cfg         *config.Config
}

// New 创建接口处理器。
func New(pods *k8s.PodService, deploys *k8s.DeployService, middlewares *k8s.MiddlewareService, overview *k8s.OverviewService, host *k8s.HostService, gpu *k8s.GPUService, cluster *k8s.ClusterService, storage *k8s.StorageService, sharedSvc *shared.Service, st *store.Store, cfg *config.Config) *Handler {
	return &Handler{Pods: pods, DeploySvc: deploys, Middlewares: middlewares, OverviewSvc: overview, HostSvc: host, GPUSvc: gpu, ClusterSvc: cluster, StorageSvc: storage, SharedSvc: sharedSvc, AuthSvc: service.NewAuthService(st), Store: st, Cfg: cfg}
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

// currentIsAdmin 判断当前登录用户是否管理员（由 ResolveRole 中间件写入上下文）。
func currentIsAdmin(c *gin.Context) bool {
	v, _ := c.Get(middleware.CtxRole)
	role, _ := v.(string)
	return role == "admin"
}

// ensurePodAccess 校验当前用户是否有权访问该 Pod 及其工作区。
// 管理员可访问全部 Pod，普通用户仅限本人创建的 Pod。
func (h *Handler) ensurePodAccess(c *gin.Context, name string) error {
	return h.Pods.EnsureAccess(c.Request.Context(), name, currentUserID(c), currentIsAdmin(c))
}

// respondErr 把内部错误映射为合适的 HTTP 状态码。
func respondErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound), apierrors.IsNotFound(err):
		Fail(c, http.StatusNotFound, "资源不存在")
	case errors.Is(err, k8s.ErrPodForbidden):
		Fail(c, http.StatusForbidden, "无权访问该 Pod")
	case errors.Is(err, k8s.ErrWebhookForbidden):
		Fail(c, http.StatusForbidden, err.Error())
	case errors.Is(err, k8s.ErrWebhookDisabled):
		Fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, k8s.ErrOutsideWorkspace):
		// 越界访问工作区：统一返回 403 + 明确提示，前端弹窗展示
		Fail(c, http.StatusForbidden, k8s.ErrOutsideWorkspace.Error())
	case errors.Is(err, k8s.ErrEntryExists):
		Fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrDuplicate):
		Fail(c, http.StatusConflict, "资源已存在，请更换名称")
	case errors.Is(err, k8s.ErrInvalidName):
		Fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, k8s.ErrInvalidMiddleware):
		// 中间件创建参数不合法（类型、名称、版本、存储大小等）
		Fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, k8s.ErrNotRegularFile):
		Fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, k8s.ErrFileTooLarge):
		Fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, k8s.ErrFileNotFound):
		Fail(c, http.StatusNotFound, err.Error())
	// 共享目录相关错误：越界统一 403，前端弹窗提示
	case errors.Is(err, shared.ErrOutsideShared):
		Fail(c, http.StatusForbidden, err.Error())
	case errors.Is(err, shared.ErrRootLocked), errors.Is(err, shared.ErrInvalidName), errors.Is(err, shared.ErrNotRegularFile):
		Fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, shared.ErrEntryExists):
		Fail(c, http.StatusConflict, err.Error())
	case errors.Is(err, shared.ErrNotFound):
		Fail(c, http.StatusNotFound, err.Error())
	case apierrors.IsAlreadyExists(err):
		Fail(c, http.StatusConflict, "资源已存在")
	default:
		Fail(c, http.StatusInternalServerError, err.Error())
	}
}
