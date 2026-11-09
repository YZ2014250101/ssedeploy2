// Package model 定义主机健康详情页的数据结构。
package model

// HostDetail 是 GET /api/host/:nodeName 返回的完整结构。
type HostDetail struct {
	CollectedAt int64           `json:"collectedAt"` // 本次采集时间（Unix 秒）
	Node        HostBasic       `json:"node"`        // 系统信息卡片
	Realtime    HostRealtime    `json:"realtime"`    // 第一行指标卡片 + 异常与延迟卡片
	Series      HostSeries      `json:"series"`      // 6 个折线图共用的一份时序数据
	Partitions  []DiskPartition `json:"partitions"`  // 磁盘使用卡片
	Nodes       []NodeBrief     `json:"nodes"`       // K3s 节点列表卡片
	GPU         HostGPU         `json:"gpu"`         // GPU 汇总卡片
}

// NodeHealth 是 GET /api/node/health 返回的系统平均负载。
// 数据来源为 /proc/loadavg 的前三个浮点数，与 CPU 使用率不是同一概念。
type NodeHealth struct {
	Load1  float64 `json:"load1"`  // 最近 1 分钟平均活跃任务数
	Load5  float64 `json:"load5"`  // 最近 5 分钟平均
	Load15 float64 `json:"load15"` // 最近 15 分钟平均
}

// HostBasic 对应「系统信息」卡片与页面标题。
type HostBasic struct {
	Name          string  `json:"name"`          // 节点名称
	Hostname      string  `json:"hostname"`      // 主机名
	OSImage       string  `json:"osImage"`       // 操作系统
	KernelVersion string  `json:"kernelVersion"` // 内核版本
	Architecture  string  `json:"architecture"`  // 架构，例如 amd64
	CPUCores      int     `json:"cpuCores"`      // CPU 核数
	PodCount      int     `json:"podCount"`      // 该节点上运行的 Pod 数
	UptimeSeconds uint64  `json:"uptimeSeconds"` // 运行时间（秒）
	Load1         float64 `json:"load1"`         // 1 分钟负载
	Load5         float64 `json:"load5"`         // 5 分钟负载
	Load15        float64 `json:"load15"`        // 15 分钟负载
	SwapUsedBytes uint64  `json:"swapUsedBytes"` // Swap 已用
	SwapTotal     uint64  `json:"swapTotalBytes"` // Swap 总容量
	K3sVersion    string  `json:"k3sVersion"`    // K3s 版本
}

// HostRealtime 是所有实时指标的当前值。
type HostRealtime struct {
	Available       bool    `json:"available"`       // 采集器是否可用
	CollectorNode   string  `json:"collectorNode"`   // 采集器所在节点（非该节点时其余字段为 0）
	CPUPercent      float64 `json:"cpuPercent"`      // CPU 使用率
	MemoryPercent   float64 `json:"memoryPercent"`   // 内存使用率
	MemoryUsed      uint64  `json:"memoryUsedBytes"` // 内存已用
	MemoryTotal     uint64  `json:"memoryTotalBytes"`// 内存总量
	DiskPercent     float64 `json:"diskPercent"`     // 根分区使用率
	DiskUsed        uint64  `json:"diskUsedBytes"`   // 根分区已用
	DiskTotal       uint64  `json:"diskTotalBytes"`  // 根分区总量
	NetRxBps        float64 `json:"netRxBps"`        // 下行速率（字节/秒）
	NetTxBps        float64 `json:"netTxBps"`        // 上行速率（字节/秒）
	DiskReadBps     float64 `json:"diskReadBps"`     // 磁盘读速率（字节/秒）
	DiskWriteBps    float64 `json:"diskWriteBps"`    // 磁盘写速率（字节/秒）
	PktRx           float64 `json:"pktRx"`           // 接收包速率（包/秒）
	PktTx           float64 `json:"pktTx"`           // 发送包速率（包/秒）
	IOPSRead        float64 `json:"iopsRead"`        // 读 IOPS
	IOPSWrite       float64 `json:"iopsWrite"`       // 写 IOPS
	RxDropPercent   float64 `json:"rxDropPercent"`   // 接收丢包率（%）
	TxDropPercent   float64 `json:"txDropPercent"`   // 发送丢包率（%）
	DiskAwaitMs     float64 `json:"diskAwaitMs"`     // 磁盘平均等待时间（毫秒）
	DiskBusyPercent float64 `json:"diskBusyPercent"` // 磁盘繁忙度（%）
}

// MetricSample 是一个采样点，6 个折线图共用。
type MetricSample struct {
	TS              int64   `json:"ts"`              // 采样时间（Unix 秒）
	CPUPercent      float64 `json:"cpuPercent"`      // 主机 CPU 使用率
	MemoryPercent   float64 `json:"memoryPercent"`   // 主机内存使用率
	NetRxBps        float64 `json:"netRxBps"`        // 网络下行
	NetTxBps        float64 `json:"netTxBps"`        // 网络上行
	DiskReadBps     float64 `json:"diskReadBps"`     // 磁盘读
	DiskWriteBps    float64 `json:"diskWriteBps"`    // 磁盘写
	PktRx           float64 `json:"pktRx"`           // 接收包速率
	PktTx           float64 `json:"pktTx"`           // 发送包速率
	IOPSRead        float64 `json:"iopsRead"`        // 读 IOPS
	IOPSWrite       float64 `json:"iopsWrite"`       // 写 IOPS
	RxDropPercent   float64 `json:"rxDropPercent"`   // 接收丢包率
	TxDropPercent   float64 `json:"txDropPercent"`   // 发送丢包率
	DiskAwaitMs     float64 `json:"diskAwaitMs"`     // 磁盘 await
	DiskBusyPercent float64 `json:"diskBusyPercent"` // 磁盘繁忙度
}

// HostSeries 是一份时序数据，最多保留 120 个采样点。
type HostSeries struct {
	MaxPoints int            `json:"maxPoints"` // 采样点上限，前端用于展示「近120采样点」
	Samples   []MetricSample `json:"samples"`   // 按时间升序
}

// DiskPartition 对应「磁盘使用」卡片中的一行。
type DiskPartition struct {
	Mount      string  `json:"mount"`      // 挂载路径
	UsedBytes  uint64  `json:"usedBytes"`  // 已用容量
	TotalBytes uint64  `json:"totalBytes"` // 总容量
	Percent    float64 `json:"percent"`    // 使用率
}

// NodeBrief 对应「K3s 节点列表」中的一项。
type NodeBrief struct {
	Name           string `json:"name"`           // 节点名称
	Ready          bool   `json:"ready"`          // Ready 状态（绿色圆点）
	DiskPressure   bool   `json:"diskPressure"`   // DiskPressure 压力
	MemoryPressure bool   `json:"memoryPressure"` // MemoryPressure 压力
	PIDPressure    bool   `json:"pidPressure"`    // PIDPressure 压力
	Version        string `json:"version"`        // K3s 版本
}

// HostGPU 对应「GPU 汇总」卡片。
type HostGPU struct {
	Count         int       `json:"count"`         // GPU 总数
	DriverVersion string    `json:"driverVersion"` // 驱动版本
	Items         []GPUCard `json:"items"`         // 每张卡的信息（复用概览的数据结构）
}
