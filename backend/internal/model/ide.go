package model

// IDEPod 是 IDE 页面 Pod 下拉选择器所需的精简信息。
// 只返回当前登录用户自己的 Pod，避免越权访问。
type IDEPod struct {
	Name       string `json:"name"`       // 平台语义上的 Pod 名称（底层 Deployment 名）
	Status     string `json:"status"`     // running / stopped / failed / notfound
	StatusText string `json:"statusText"` // 中文状态文案
	PodName    string `json:"podName"`    // 真实 Pod 名称，未运行时为空
	Running    bool   `json:"running"`    // 是否可用于终端与日志
}
