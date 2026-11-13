package model

// Pod 的四种展示状态，与前端状态徽章一一对应。
const (
	StatusRunning  = "running"  // 运行中：副本数 > 0 且未检测到故障（含正在启动）
	StatusStopped  = "stopped"  // 已停止：Deployment 副本数为 0
	StatusFailed   = "failed"   // 失败：镜像拉取失败、CrashLoop、Pod Failed
	StatusNotFound = "notfound" // NotFound：Deployment 在集群中已不存在
)

// StatusText 把状态码翻译成界面展示的中文文案。
func StatusText(status string) string {
	switch status {
	case StatusRunning:
		return "运行中"
	case StatusStopped:
		return "已停止"
	case StatusFailed:
		return "失败"
	default:
		return "NotFound"
	}
}

// EnvVar 环境变量键值对。
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// PodMeta 是平台侧的 Pod 元数据，用来保存 K8s 里没有的业务信息（负责人、存储规格等）。
// 一条记录对应一个 K8s Deployment。
type PodMeta struct {
	ID        int64    `json:"id"`
	Namespace string   `json:"namespace"`
	Name      string   `json:"name"`
	OwnerID   int64    `json:"ownerId"`
	Image     string   `json:"image"`
	CPU       string   `json:"cpu"`
	Memory    string   `json:"memory"`
	Disk      string   `json:"disk"`
	Replicas  int32    `json:"replicas"`
	Env       []EnvVar `json:"env"`
	Command   []string `json:"command"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

// PodView 列表页每一行展示的数据。
type PodView struct {
	Name          string `json:"name"`
	Status        string `json:"status"`
	StatusText    string `json:"statusText"`
	Spec          string `json:"spec"`
	Image         string `json:"image"`
	Owner         string `json:"owner"`
	MyRole        string `json:"myRole"`
	Replicas      int32  `json:"replicas"`
	ReadyReplicas int32  `json:"readyReplicas"`
	CreatedAt     int64  `json:"createdAt"`
}

// PodDetail 详情页数据，在列表字段基础上补充可编辑配置。
type PodDetail struct {
	PodView
	Namespace string   `json:"namespace"`
	CPU       string   `json:"cpu"`
	Memory    string   `json:"memory"`
	Disk      string   `json:"disk"`
	Env       []EnvVar `json:"env"`
	Command   []string `json:"command"`
	PodName   string   `json:"podName"` // 真实 Pod 名（用于日志/终端）
	// 外部访问 Service：创建 Pod 时自动生成并分配 NodePort
	ServiceName string `json:"serviceName"`
	ServicePort int32  `json:"servicePort"` // Service 暴露的容器端口
	NodePort    int32  `json:"nodePort"`    // 自动分配的宿主端口
}

// CreatePodRequest 创建 Pod 的入参。
type CreatePodRequest struct {
	Name     string   `json:"name"`
	Image    string   `json:"image"`
	CPU      string   `json:"cpu"`
	Memory   string   `json:"memory"`
	Disk     string   `json:"disk"`
	Replicas int32    `json:"replicas"`
	Env      []EnvVar `json:"env"`
	Command  []string `json:"command"`
}

// UpdatePodRequest 设置页保存的入参（名称、存储、副本不支持在此修改）。
type UpdatePodRequest struct {
	Image   string   `json:"image"`
	CPU     string   `json:"cpu"`
	Memory  string   `json:"memory"`
	Env     []EnvVar `json:"env"`
	Command []string `json:"command"`
}

// MetricPoint 单次采样的资源用量。
type MetricPoint struct {
	TS       int64 `json:"ts"`       // Unix 秒
	CPUMilli int64 `json:"cpuMilli"` // CPU 毫核
	MemBytes int64 `json:"memBytes"` // 内存字节
}

// EventView 最近事件，来源于 K8s Event。
type EventView struct {
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Count    int32  `json:"count"`
	LastTime int64  `json:"lastTime"`
}
