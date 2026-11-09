package model

// ClusterNode 是集群页节点卡片的数据结构。
type ClusterNode struct {
	NodeName   string  `json:"nodeName"`
	Status     string  `json:"status"` // online / offline
	CpuPercent float64 `json:"cpuPercent"`
	MemPercent float64 `json:"memPercent"`
	Load       float64 `json:"load"`
}

// ClusterSeries 是集群全局 CPU / 内存 / 负载时序。
// map 的 key 为节点名，value 与 timestamps 一一对应。
type ClusterSeries struct {
	Timestamps []string             `json:"timestamps"`
	CPU        map[string][]float64 `json:"cpu"`
	Mem        map[string][]float64 `json:"mem"`
	Load       map[string][]float64 `json:"load"`
}

// GpuItem 是节点的 GPU 简要信息。
type GpuItem struct {
	UUID string `json:"uuid"`
}

// GpuSeries 是单节点 GPU 利用率 / 显存 / 温度时序。
// map 的 key 为 GPU UUID，value 与 timestamps 一一对应。
type GpuSeries struct {
	Timestamps []string             `json:"timestamps"`
	Util       map[string][]float64 `json:"util"`
	MemUsed    map[string][]float64 `json:"memUsed"`
	Temp       map[string][]float64 `json:"temp"`
}
