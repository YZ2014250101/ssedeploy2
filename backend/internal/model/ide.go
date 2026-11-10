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

// IDEFileEntry 是工作区中的一个文件或目录（相对工作区根的路径）。
// 文件树、编辑器与终端共享同一份 PVC 数据，路径都以工作区根为基准。
type IDEFileEntry struct {
	Path string `json:"path"` // 相对路径，例如 main.go、pkg/util.go
	Type string `json:"type"` // file / dir
}
