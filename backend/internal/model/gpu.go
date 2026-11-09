// Package model 定义 GPU 监控页的数据结构。
package model

// GPUOverview 是 GET /api/gpu 返回的完整结构。
type GPUOverview struct {
	CollectedAt int64         `json:"collectedAt"` // 本次采集时间（Unix 秒）
	Summary     GPUPageSummary `json:"summary"`    // 顶部 4 个统计卡片
	GPUList     []GPUDevice   `json:"gpuList"`     // 每块 GPU 卡片
	Trend       GPUTrend      `json:"trend"`       // 每块卡的利用率 / 显存时序
	PodAlloc    []GPUPodAlloc `json:"podAlloc"`    // 每块卡的 Pod 占用
}

// GPUPageSummary 对应页面顶部汇总信息。
type GPUPageSummary struct {
	Count         int     `json:"count"`          // GPU 总数
	AvgUtil       float64 `json:"avgUtil"`        // 平均利用率 %
	MemUsed       uint64  `json:"memUsedBytes"`   // 显存已用
	MemTotal      uint64  `json:"memTotalBytes"`  // 显存总量
	MaxTemp       int     `json:"maxTemp"`        // 最高温度 ℃
	DriverVersion string  `json:"driverVersion"`  // 驱动版本
}

// GPUDevice 对应单块 GPU 卡片头部与实时指标。
type GPUDevice struct {
	ID       int     `json:"id"`           // GPU 编号
	Name     string  `json:"name"`         // 显卡型号
	Temp     int     `json:"temp"`         // 当前温度 ℃
	Power    float64 `json:"power"`        // 当前功耗 W
	PowerMax float64 `json:"powerMax"`     // 功耗上限 W
	Util     float64 `json:"util"`         // 利用率 %
	MemUsed  uint64  `json:"memUsedBytes"` // 显存已用
	MemTotal uint64  `json:"memTotalBytes"`// 显存总量
}

// GPUTrendPoint 是趋势图上的一个采样点。
type GPUTrendPoint struct {
	TS    int64   `json:"ts"`    // 采样时间（Unix 秒）
	Value float64 `json:"value"` // 利用率 % 或显存字节数
}

// GPUTrendSeries 是单块 GPU 的两条时序曲线。
type GPUTrendSeries struct {
	GPUId int             `json:"gpuId"` // 对应 GPUDevice.ID
	Util  []GPUTrendPoint `json:"util"`  // 利用率时序
	Mem   []GPUTrendPoint `json:"mem"`   // 显存时序（字节）
}

// GPUTrend 是所有 GPU 的时序集合。
type GPUTrend struct {
	MaxPoints int              `json:"maxPoints"` // 采样点上限 120
	Series    []GPUTrendSeries `json:"series"`    // 按 GPU 编号排列
}

// PodGPUUsage 是某个 Pod 在一块 GPU 上的占用。
type PodGPUUsage struct {
	Name      string  `json:"name"`          // Pod 名称
	Namespace string  `json:"namespace"`     // 命名空间
	MemUsed   uint64  `json:"memUsedBytes"`  // 占用显存
	Percent   float64 `json:"percent"`       // 占该卡显存比例 %
	Util      float64 `json:"util"`          // 该 Pod 的利用率 %
}

// GPUPodAlloc 是单块 GPU 上的 Pod 分配情况。
type GPUPodAlloc struct {
	GPUId int           `json:"gpuId"` // 对应 GPUDevice.ID
	Pods  []PodGPUUsage `json:"pods"`  // 已分配 Pod 列表
}
