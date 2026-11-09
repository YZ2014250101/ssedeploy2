// Package model 定义概览页的数据结构，字段与前端卡片一一对应。
package model

// Overview 是概览页一次性聚合返回的全部数据。
type Overview struct {
	UpdatedAt int64       `json:"updatedAt"` // 数据采集时间（Unix 秒）
	Pods      PodSummary  `json:"pods"`      // 运行 Pod 统计
	CPU       CPUStat     `json:"cpu"`       // CPU 负载
	Memory    UsageStat   `json:"memory"`    // 内存使用
	Disk      UsageStat   `json:"disk"`      // 根分区磁盘使用
	GPU       GPUSummary  `json:"gpu"`       // GPU 列表
	Cluster   ClusterStat `json:"cluster"`   // 集群信息卡片
	Modules   ModuleStat  `json:"modules"`   // 基础设施 / 开发 / 攻防 模块卡片
}

// PodSummary 对应「运行 Pod」统计卡，展示格式为 当前数 / 总数。
type PodSummary struct {
	Running int `json:"running"` // 处于 Running 的 Pod 数
	Total   int `json:"total"`   // 集群内 Pod 总数
	Stopped int `json:"stopped"` // 已停止（Succeeded/Failed）的 Pod 数
}

// CPUStat 对应「CPU 负载(1m)」统计卡。
type CPUStat struct {
	Load1m    float64 `json:"load1m"`    // 1 分钟平均负载
	Cores     int     `json:"cores"`     // CPU 核心数
	Available bool    `json:"available"` // 是否成功读取到宿主机 /proc 数据
}

// UsageStat 对应带进度条的「内存使用」「磁盘使用(/)」统计卡。
type UsageStat struct {
	Percent    float64 `json:"percent"`    // 使用率百分比，0~100
	UsedBytes  int64   `json:"usedBytes"`  // 已用字节
	TotalBytes int64   `json:"totalBytes"` // 总字节
	Available  bool    `json:"available"`  // 是否成功读取到数据源
}

// GPUSummary 描述 GPU 总数与每张卡的信息。
type GPUSummary struct {
	Count int       `json:"count"` // GPU 总数量，例如 3
	Items []GPUCard `json:"items"` // 每张 GPU 一张卡片
}

// GPUCard 对应单张 GPU 卡片。
type GPUCard struct {
	Index       int    `json:"index"`       // 卡编号，对应 GPU0 / GPU1 ...
	Name        string `json:"name"`        // GPU 型号
	Utilization int    `json:"utilization"` // 利用率百分比
	MemoryUsed  int64  `json:"memoryUsed"`  // 已用显存（字节）
	MemoryTotal int64  `json:"memoryTotal"` // 总显存（字节）
	Temperature int    `json:"temperature"` // 温度（摄氏度）
}

// ClusterStat 对应「集群信息」卡片。
type ClusterStat struct {
	RunningPods int    `json:"runningPods"` // 运行中 Pod 数
	TotalPods   int    `json:"totalPods"`   // Pod 总数
	StoppedPods int    `json:"stoppedPods"` // 已停止 Pod 数
	Groups      int    `json:"groups"`      // 业务分组（命名空间）数
	NodeCount   int    `json:"nodeCount"`   // K3s 节点数量
	ReadyNodes  int    `json:"readyNodes"`  // Ready 状态节点数
	Version     string `json:"version"`     // K3s 版本（取自节点 Kubelet 版本）
}

// ModuleStat 对应「基础设施 / 开发 / 攻防」三个模块卡片。
type ModuleStat struct {
	Nodes        int  `json:"nodes"`        // 基础设施：节点数量
	NodesOnline  bool `json:"nodesOnline"`  // 节点是否全部在线（绿色圆点）
	Agents       int  `json:"agents"`       // 开发：Agent（运行中的开发环境）数量
	AgentsOnline bool `json:"agentsOnline"` // Agent 是否在线（绿色圆点）
	Attacks      int  `json:"attacks"`      // 攻防：攻击数量
	Bans         int  `json:"bans"`         // 攻防：封禁数量
}
