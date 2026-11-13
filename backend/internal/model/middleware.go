package model

// 中间件类型。新增中间件（如 Redis、PostgreSQL）时在此追加，
// 并在 k8s/middleware_service.go 的 registry 中补充对应的部署形态。
const (
	MiddlewareMySQL = "mysql"
	MiddlewareMinIO = "minio"
)

// 中间件实例状态：由 K8s 侧实际状态实时计算，不落库。
const (
	MiddlewareRunning  = "running"  // 运行中：至少一个副本就绪
	MiddlewareCreating = "creating" // 启动中：已创建工作负载但尚未就绪
	MiddlewareFailed   = "failed"   // 失败：镜像拉取失败、CrashLoop 等
	MiddlewareNotFound = "notfound" // 集群中已不存在
)

// MiddlewareStatusText 把状态码翻译成界面文案。
func MiddlewareStatusText(status string) string {
	switch status {
	case MiddlewareRunning:
		return "运行中"
	case MiddlewareCreating:
		return "启动中"
	case MiddlewareFailed:
		return "失败"
	default:
		return "NotFound"
	}
}

// Middleware 是一个被平台纳管的中间件实例（MySQL / MinIO …）。
// PodName 表示「归属 Pod」：连接信息只在该 Pod 详情页的「连接」板块展示。
type Middleware struct {
	ID        int64  `json:"id"`
	Namespace string `json:"namespace"`
	PodName   string `json:"podName"`
	OwnerID   int64  `json:"ownerId"`
	Type      string `json:"type"`    // mysql / minio
	Name      string `json:"name"`    // 服务名，同时作为 K8s Deployment / Service 名称
	Version   string `json:"version"` // 镜像版本，例如 8.0
	Image     string `json:"image"`   // 完整镜像地址
	Storage   string `json:"storage"` // 数据盘大小，例如 10Gi
	Username  string `json:"username"`
	Password  string `json:"password"` // 创建时自动生成的高强度密码
	Database  string `json:"database"` // 初始库名（MySQL）
	Port      int32  `json:"port"`     // Service 暴露端口
	Host      string `json:"host"`     // 集群内访问域名（<name>.<ns>.svc.cluster.local）
	Status    string `json:"status"`   // running / creating / failed / notfound
	CreatedAt int64  `json:"createdAt"`
}

// CreateMiddlewareRequest 创建中间件的入参。
type CreateMiddlewareRequest struct {
	Type     string `json:"type"`     // mysql / minio，缺省 mysql
	Name     string `json:"name"`     // 服务名，需满足 DNS-1123
	Version  string `json:"version"`  // 镜像版本，缺省取该类型的默认版本
	Storage  string `json:"storage"`  // 存储大小，缺省 5Gi
	Database string `json:"database"` // MySQL 初始库名，缺省 app
}
