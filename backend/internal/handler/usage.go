package handler

import (
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// usageSteps 各时间范围的数据点数量。
// 24h / 7d 按小时聚合，30d 按天聚合。
var usageSteps = map[string]int{"24h": 24, "7d": 168, "30d": 30}

// usageHoursPerStep 每个聚合桶代表的小时数，用于把「每桶均值」换算成核时 / GiB·h。
var usageHoursPerStep = map[string]float64{"24h": 1, "7d": 1, "30d": 24}

// usageCard 顶部概览卡片。
type usageCard struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Value       float64 `json:"value"`
	Unit        string  `json:"unit"`
	Utilization float64 `json:"utilization,omitempty"` // 仅 CPU 利用率卡片使用（百分比）
}

// usagePoint 图表与明细表共用的一条聚合记录。
type usagePoint struct {
	TS            int64   `json:"ts"`
	Time          string  `json:"time"`
	CPUUsed       float64 `json:"cpuUsed"`
	CPURequested  float64 `json:"cpuRequested"`
	GPUMem        float64 `json:"gpuMem"`
	GPUCardHours  float64 `json:"gpuCardHours"`
	Memory        float64 `json:"memory"`
	StorageDir    float64 `json:"storageDir"`
	StorageBucket float64 `json:"storageBucket"`
}

// PodUsage 返回 Pod 用量面板数据。
// 当前为 Mock 实现：同一 Pod + 时间范围生成稳定数据，便于前端联调；
// 后续接入 Prometheus / 计费库时只需替换 buildUsageMock。
func (h *Handler) PodUsage(c *gin.Context) {
	podName := c.Param("name")
	if err := h.ensurePodAccess(c, podName); err != nil {
		respondErr(c, err)
		return
	}

	rangeKey := c.DefaultQuery("range", "7d")
	if _, ok := usageSteps[rangeKey]; !ok {
		Fail(c, http.StatusBadRequest, "range 仅支持 24h / 7d / 30d")
		return
	}

	OK(c, buildUsageMock(podName, rangeKey))
}

// buildUsageMock 构造 Mock 用量数据：
//   - CPU 申请核时长期稳定（模拟预留资源），实测核时大部分为 0、偶有小值；
//   - 内存、存储按小时累计，使概览卡片与明细表求和一致。
func buildUsageMock(podName, rangeKey string) gin.H {
	steps := usageSteps[rangeKey]
	hours := usageHoursPerStep[rangeKey]

	// 用 Pod 名 + 范围做种子，保证同一 Pod 每次刷新数据一致
	seed := int64(2166136261)
	for _, ch := range podName + "|" + rangeKey {
		seed = (seed ^ int64(ch)) * 16777619
	}
	random := rand.New(rand.NewSource(seed))

	now := time.Now()
	points := make([]usagePoint, 0, steps)
	var sumUsed, sumRequested, sumGPUMem, sumGPUCard, sumMemory, sumDir, sumBucket float64

	for i := 0; i < steps; i++ {
		ts := now.Add(-time.Duration(float64(steps-1-i) * hours * float64(time.Hour)))
		label := ts.Format("15:04")
		if rangeKey == "30d" {
			label = ts.Format("01-02")
		}

		// 申请核数约 0.075 核（7 天累计约 12.7 核时）
		requested := roundTo(0.0755+random.Float64()*0.0004, 4)
		// 实测核时大部分为 0，少量时段有极短的计算，累计约 0.03 核时
		used := 0.0
		if random.Float64() < 0.12 {
			used = roundTo(0.0002+random.Float64()*0.0009, 4)
		}
		memory := roundTo(0.019+random.Float64()*0.004, 4)       // GiB
		storageDir := roundTo(0.0028+random.Float64()*0.0008, 4) // GiB
		gpuMem := 0.0                                            // 无 GPU 的 Pod 恒为 0
		gpuCard := 0.0

		sumUsed += used * hours
		sumRequested += requested * hours
		sumGPUMem += gpuMem * hours
		sumGPUCard += gpuCard * hours
		sumMemory += memory * hours
		sumDir += storageDir * hours

		points = append(points, usagePoint{
			TS:            ts.Unix(),
			Time:          label,
			CPUUsed:       used,
			CPURequested:  requested,
			GPUMem:        gpuMem,
			GPUCardHours:  gpuCard,
			Memory:        memory,
			StorageDir:    storageDir,
			StorageBucket: 0,
		})
	}

	// CPU 利用率 = 实测核时 / 申请核时，避免除零
	utilization := 0.0
	if sumRequested > 0 {
		utilization = roundTo(sumUsed/sumRequested*100, 1)
	}

	cards := []usageCard{
		{Key: "cpu_used", Title: "CPU 实测核时", Value: roundTo(sumUsed, 2), Unit: "核时"},
		{Key: "cpu_requested", Title: "CPU 申请核时", Value: roundTo(sumRequested, 2), Unit: "核时"},
		{Key: "cpu_utilization", Title: "CPU 利用率", Value: utilization, Unit: "%", Utilization: utilization},
		{Key: "gpu_memory", Title: "GPU 显存", Value: roundTo(sumGPUMem, 2), Unit: "GiB·h"},
		{Key: "memory", Title: "内存", Value: roundTo(sumMemory, 2), Unit: "GiB·h"},
		{Key: "storage_dir", Title: "存储·组目录", Value: roundTo(sumDir, 2), Unit: "GiB·h"},
		{Key: "storage_bucket", Title: "存储·对象桶", Value: roundTo(sumBucket, 2), Unit: "GiB·h"},
		{Key: "gpu_card", Title: "算力卡时", Value: roundTo(sumGPUCard, 2), Unit: "卡时"},
	}

	return gin.H{"range": rangeKey, "cards": cards, "points": points}
}

// roundTo 四舍五入到指定小数位。
func roundTo(value float64, digits int) float64 {
	base := math.Pow(10, float64(digits))
	return math.Round(value*base) / base
}
