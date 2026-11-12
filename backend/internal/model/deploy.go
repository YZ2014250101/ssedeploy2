package model

// 部署实例状态：
//   - pending   创建中 / 启动中
//   - running   运行中
//   - stopped   已停止
//   - fatal     运行致命错误（服务启动失败、镜像拉取失败、CrashLoop 等）
//   - succeeded 一次性脚本执行完成
//   - notfound  集群中的工作负载已被删除
const (
	DeployStatusPending   = "pending"
	DeployStatusRunning   = "running"
	DeployStatusStopped   = "stopped"
	DeployStatusFatal     = "fatal"
	DeployStatusSucceeded = "succeeded"
	DeployStatusNotFound  = "notfound"
)

// 部署来源：绑仓库（从 Git 拉取代码） / 本地（平台内置运行时，不拉代码）
const (
	DeploySourceRepo  = "repo"
	DeploySourceLocal = "local"
)

// 部署类型：常驻服务 / 一次性脚本
const (
	DeployTypeService = "service"
	DeployTypeJob     = "job"
)

// DeployStatusText 把部署状态码翻译成界面文案。
func DeployStatusText(status string) string {
	switch status {
	case DeployStatusPending:
		return "启动中"
	case DeployStatusRunning:
		return "运行中"
	case DeployStatusStopped:
		return "已停止"
	case DeployStatusFatal:
		return "fatal"
	case DeployStatusSucceeded:
		return "执行完成"
	default:
		return "NotFound"
	}
}

// DeploySourceText 把部署来源翻译成界面标签文案。
func DeploySourceText(source string) string {
	if source == DeploySourceLocal {
		return "本地"
	}
	return "Git仓库"
}

// DeployTypeText 把部署类型翻译成界面文案。
func DeployTypeText(t string) string {
	if t == DeployTypeJob {
		return "一次性脚本"
	}
	return "常驻服务"
}

// Deploy 部署实例，对应 deployments 表。
// Token 属于敏感信息，仅入库不回传前端（json:"-"）。
type Deploy struct {
	ID            int64  `json:"id"`
	Namespace     string `json:"namespace"`
	PodName       string `json:"podName"` // 部署所属的 Pod（部署脚本在该 Pod 内执行）
	Name          string `json:"name"`
	OwnerID       int64  `json:"ownerId"`
	Source        string `json:"source"`       // repo / local
	RepoURL       string `json:"repoUrl"`      // Git 仓库地址
	Branch        string `json:"branch"`       // 构建分支
	SubDir        string `json:"subDir"`       // 子目录，根目录为空
	Type          string `json:"type"`         // service / job
	ScheduleType  string `json:"scheduleType"` // inference：推理服务，走推理资源池
	GPU           string `json:"gpu"`          // 绑卡 GPU 标识，空表示不绑定
	Token         string `json:"-"`            // 私有库访问令牌
	HealthPath    string `json:"healthPath"`   // 健康检查路径，空表示跳过探测
	TimeoutSec    int    `json:"timeoutSec"`   // 健康检查超时（秒）
	AutoDeploy    bool   `json:"autoDeploy"`   // 推送自动部署开关
	WebhookSecret string `json:"-"`            // Webhook 回调密钥，仅入库
	Status        string `json:"status"`
	Message       string `json:"message"` // 状态补充说明（失败原因等）
	CreatedAt     int64  `json:"createdAt"`
	UpdatedAt     int64  `json:"updatedAt"`
}

// DeployView 部署列表 / 卡片展示数据。
type DeployView struct {
	PodName      string `json:"podName"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	StatusText   string `json:"statusText"`
	Message      string `json:"message"`
	Source       string `json:"source"`
	SourceText   string `json:"sourceText"`
	RepoURL      string `json:"repoUrl"`
	Branch       string `json:"branch"`
	SubDir       string `json:"subDir"`
	Type         string `json:"type"`
	TypeText     string `json:"typeText"`
	ScheduleType string `json:"scheduleType"`
	GPU          string `json:"gpu"`
	HealthPath   string `json:"healthPath"`
	TimeoutSec   int    `json:"timeoutSec"`
	AutoDeploy   bool   `json:"autoDeploy"`
	WebhookPath  string `json:"webhookPath"`  // Webhook 回调路径（不含域名与密钥）
	WebhookToken string `json:"webhookToken"` // 回调密钥，仅负责人 / 管理员可见
	Owner        string `json:"owner"`
	MyRole       string `json:"myRole"`
	CreatedAt    int64  `json:"createdAt"`
}

// CreateDeployRequest 创建部署入参。
type CreateDeployRequest struct {
	Source       string `json:"source"`
	Name         string `json:"name"`
	RepoURL      string `json:"repoUrl"`
	Branch       string `json:"branch"`
	SubDir       string `json:"subDir"`
	Type         string `json:"type"`
	ScheduleType string `json:"scheduleType"`
	GPU          string `json:"gpu"`
	Token        string `json:"token"`
	HealthPath   string `json:"healthPath"`
	TimeoutSec   int    `json:"timeoutSec"`
	AutoDeploy   bool   `json:"autoDeploy"`
}

// UpdateDeployRequest 编辑部署入参（名称不可修改）。
type UpdateDeployRequest struct {
	RepoURL      string `json:"repoUrl"`
	Branch       string `json:"branch"`
	SubDir       string `json:"subDir"`
	Type         string `json:"type"`
	ScheduleType string `json:"scheduleType"`
	GPU          string `json:"gpu"`
	Token        string `json:"token"`
	HealthPath   string `json:"healthPath"`
	TimeoutSec   int    `json:"timeoutSec"`
	AutoDeploy   bool   `json:"autoDeploy"`
}
