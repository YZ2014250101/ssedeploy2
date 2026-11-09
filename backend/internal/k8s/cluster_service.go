package k8s

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
)

const (
	// clusterSampleInterval 集群指标采样间隔；15 秒可覆盖 15m 粒度且有足够点位。
	clusterSampleInterval = 15 * time.Second
	// clusterMaxPoints 最长保留 7 天数据（7*24*3600/15 = 40320）。
	clusterMaxPoints = 40320
)

// nodeSample 是一次节点采样值。
type nodeSample struct {
	cpu  float64
	mem  float64
	load float64
}

// clusterPoint 是节点时序中的一个点。
type clusterPoint struct {
	ts   int64
	cpu  float64
	mem  float64
	load float64
}

// gpuPoint 是单块 GPU 时序中的一个点（显存单位为 GB）。
type gpuPoint struct {
	ts    int64
	util  float64
	memGB float64
	temp  float64
}

// gpuReading 是 nvidia-smi 的一次读取结果。
type gpuReading struct {
	uuid  string
	util  float64
	memGB float64
	temp  float64
}

// ClusterService 提供集群页所需的节点、集群时序与 GPU 时序数据。
// 历史数据由后台采样器持续写入内存环形缓冲，重启后重新累积。
type ClusterService struct {
	client *Client

	mu         sync.Mutex
	history    map[string][]clusterPoint // 节点名 -> 时序
	gpuHistory map[string][]gpuPoint     // GPU UUID -> 时序
	gpuNode    string                    // GPU 采集所在节点（后端所在节点）
}

// NewClusterService 创建集群服务。
func NewClusterService(client *Client) *ClusterService {
	return &ClusterService{
		client:     client,
		history:    make(map[string][]clusterPoint),
		gpuHistory: make(map[string][]gpuPoint),
	}
}

// Start 启动后台采样：节点 CPU/内存/负载 + 本机 GPU 指标。
func (s *ClusterService) Start(ctx context.Context) {
	go func() {
		s.sample()
		ticker := time.NewTicker(clusterSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sample()
			}
		}
	}()
}

// sample 采集一轮节点与 GPU 指标并写入内存。
func (s *ClusterService) sample() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	_, samples, selfNode, err := s.collectNodes(ctx)
	if err != nil {
		return
	}
	now := time.Now().Unix()

	s.mu.Lock()
	for name, value := range samples {
		list := append(s.history[name], clusterPoint{ts: now, cpu: value.cpu, mem: value.mem, load: value.load})
		if len(list) > clusterMaxPoints {
			list = list[len(list)-clusterMaxPoints:]
		}
		s.history[name] = list
	}
	s.gpuNode = selfNode
	s.mu.Unlock()

	// GPU 只能从后端所在节点通过 nvidia-smi 采集
	if selfNode == "" {
		return
	}
	for _, reading := range readGPUSamples(ctx) {
		s.mu.Lock()
		list := append(s.gpuHistory[reading.uuid], gpuPoint{
			ts: now, util: reading.util, memGB: reading.memGB, temp: reading.temp,
		})
		if len(list) > clusterMaxPoints {
			list = list[len(list)-clusterMaxPoints:]
		}
		s.gpuHistory[reading.uuid] = list
		s.mu.Unlock()
	}
}

// collectNodes 读取节点列表与 metrics-server 指标，并计算 CPU/内存百分比。
// load 只有采集器所在节点可读（/proc/loadavg），其余节点返回 0。
func (s *ClusterService) collectNodes(ctx context.Context) ([]model.ClusterNode, map[string]nodeSample, string, error) {
	nodes, err := s.client.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, "", fmt.Errorf("读取节点失败: %w", err)
	}

	// metrics-server 不可用时节点仍可展示，CPU/内存置 0
	usage := make(map[string]corev1.ResourceList)
	if list, err := s.client.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{}); err == nil {
		for i := range list.Items {
			usage[list.Items[i].Name] = list.Items[i].Usage
		}
	}

	self := resolveSelfNode(nodes.Items)
	selfLoad := 0.0
	if self != "" {
		selfLoad = ReadNodeHealth().Load1
	}

	result := make([]model.ClusterNode, 0, len(nodes.Items))
	samples := make(map[string]nodeSample, len(nodes.Items))
	for i := range nodes.Items {
		node := &nodes.Items[i]
		cpu, mem := 0.0, 0.0
		if u, ok := usage[node.Name]; ok {
			cpu = percentOf(u.Cpu().MilliValue(), node.Status.Capacity.Cpu().MilliValue())
			mem = percentOf(u.Memory().Value(), node.Status.Capacity.Memory().Value())
		}
		load := 0.0
		if node.Name == self {
			load = selfLoad
		}
		status := "offline"
		if nodeReady(node) {
			status = "online"
		}
		result = append(result, model.ClusterNode{
			NodeName:   node.Name,
			Status:     status,
			CpuPercent: cpu,
			MemPercent: mem,
			Load:       load,
		})
		samples[node.Name] = nodeSample{cpu: cpu, mem: mem, load: load}
	}
	return result, samples, self, nil
}

// GetNodes 返回节点列表实时数据。
func (s *ClusterService) GetNodes(ctx context.Context) ([]model.ClusterNode, error) {
	nodes, _, _, err := s.collectNodes(ctx)
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// clusterRange 描述时间粒度对应的窗口、步长与时间格式。
type clusterRange struct {
	duration time.Duration
	step     time.Duration
	layout   string
}

var clusterRanges = map[string]clusterRange{
	"15m": {15 * time.Minute, 30 * time.Second, "15:04"},
	"1h":  {time.Hour, time.Minute, "15:04"},
	"6h":  {6 * time.Hour, 5 * time.Minute, "15:04"},
	"24h": {24 * time.Hour, 20 * time.Minute, "01-02 15:04"},
	"7d":  {7 * 24 * time.Hour, 2 * time.Hour, "01-02 15:04"},
}

// resolveRange 返回合法的粒度配置，非法值回退为 15m。
func resolveRange(raw string) clusterRange {
	if spec, ok := clusterRanges[raw]; ok {
		return spec
	}
	return clusterRanges["15m"]
}

// GetMetrics 返回集群全局 CPU / 内存 / 负载时序（按粒度聚合）。
func (s *ClusterService) GetMetrics(_ context.Context, rawRange string) *model.ClusterSeries {
	spec := resolveRange(rawRange)
	now := time.Now()
	start := now.Add(-spec.duration)
	count := int(spec.duration/spec.step) + 1

	timestamps := make([]string, 0, count)
	for i := 0; i < count; i++ {
		timestamps = append(timestamps, start.Add(time.Duration(i)*spec.step).Format(spec.layout))
	}

	// 复制一份历史，避免长时间持锁
	s.mu.Lock()
	history := make(map[string][]clusterPoint, len(s.history))
	for name, points := range s.history {
		history[name] = append([]clusterPoint(nil), points...)
	}
	s.mu.Unlock()

	result := &model.ClusterSeries{
		Timestamps: timestamps,
		CPU:        make(map[string][]float64, len(history)),
		Mem:        make(map[string][]float64, len(history)),
		Load:       make(map[string][]float64, len(history)),
	}
	for name, points := range history {
		result.CPU[name] = bucketAverages(points, func(p clusterPoint) int64 { return p.ts },
			func(p clusterPoint) float64 { return p.cpu }, start, spec.step, count)
		result.Mem[name] = bucketAverages(points, func(p clusterPoint) int64 { return p.ts },
			func(p clusterPoint) float64 { return p.mem }, start, spec.step, count)
		result.Load[name] = bucketAverages(points, func(p clusterPoint) int64 { return p.ts },
			func(p clusterPoint) float64 { return p.load }, start, spec.step, count)
	}
	return result
}

// GetGPUs 返回指定节点的 GPU UUID 列表；非采集器节点返回空列表。
func (s *ClusterService) GetGPUs(ctx context.Context, nodeName string) []model.GpuItem {
	items := []model.GpuItem{}

	nodes, err := s.client.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return items
	}
	if nodeName == "" || nodeName != resolveSelfNode(nodes.Items) {
		return items
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "nvidia-smi",
		"--query-gpu=uuid", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return items
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if uuid := strings.TrimSpace(line); uuid != "" {
			items = append(items, model.GpuItem{UUID: uuid})
		}
	}
	return items
}

// GetGPUMetrics 返回指定节点 GPU 时序；非采集器节点返回空结构。
func (s *ClusterService) GetGPUMetrics(_ context.Context, nodeName, rawRange string) *model.GpuSeries {
	spec := resolveRange(rawRange)
	empty := func() *model.GpuSeries {
		return &model.GpuSeries{
			Timestamps: []string{},
			Util:       map[string][]float64{},
			MemUsed:    map[string][]float64{},
			Temp:       map[string][]float64{},
		}
	}

	s.mu.Lock()
	gpuNode := s.gpuNode
	history := make(map[string][]gpuPoint, len(s.gpuHistory))
	for uuid, points := range s.gpuHistory {
		history[uuid] = append([]gpuPoint(nil), points...)
	}
	s.mu.Unlock()

	if nodeName == "" || nodeName != gpuNode || len(history) == 0 {
		return empty()
	}

	now := time.Now()
	start := now.Add(-spec.duration)
	count := int(spec.duration/spec.step) + 1
	timestamps := make([]string, 0, count)
	for i := 0; i < count; i++ {
		timestamps = append(timestamps, start.Add(time.Duration(i)*spec.step).Format(spec.layout))
	}

	result := &model.GpuSeries{
		Timestamps: timestamps,
		Util:       make(map[string][]float64, len(history)),
		MemUsed:    make(map[string][]float64, len(history)),
		Temp:       make(map[string][]float64, len(history)),
	}
	for uuid, points := range history {
		result.Util[uuid] = bucketAverages(points, func(p gpuPoint) int64 { return p.ts },
			func(p gpuPoint) float64 { return p.util }, start, spec.step, count)
		result.MemUsed[uuid] = bucketAverages(points, func(p gpuPoint) int64 { return p.ts },
			func(p gpuPoint) float64 { return p.memGB }, start, spec.step, count)
		result.Temp[uuid] = bucketAverages(points, func(p gpuPoint) int64 { return p.ts },
			func(p gpuPoint) float64 { return p.temp }, start, spec.step, count)
	}
	return result
}

// readGPUSamples 调用 nvidia-smi 读取每块 GPU 的利用率、显存（GB）与温度。
func readGPUSamples(ctx context.Context) []gpuReading {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cmdCtx, "nvidia-smi",
		"--query-gpu=uuid,utilization.gpu,memory.used,temperature.gpu",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}

	readings := make([]gpuReading, 0, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 4 {
			continue
		}
		uuid := strings.TrimSpace(fields[0])
		if uuid == "" {
			continue
		}
		readings = append(readings, gpuReading{
			uuid:  uuid,
			util:  parseFloatSafe(fields[1]),
			memGB: parseFloatSafe(fields[2]) / 1024, // nvidia-smi 显存单位为 MiB
			temp:  parseFloatSafe(fields[3]),
		})
	}
	return readings
}

// bucketAverages 把时序点按步长聚合成固定长度数组，空桶填 0。
func bucketAverages[T any](points []T, timestamp func(T) int64, value func(T) float64,
	start time.Time, step time.Duration, count int) []float64 {
	sums := make([]float64, count)
	counts := make([]int, count)
	stepSeconds := int64(step.Seconds())
	if stepSeconds <= 0 {
		return make([]float64, count)
	}
	for _, point := range points {
		index := int((timestamp(point) - start.Unix()) / stepSeconds)
		if index < 0 || index >= count {
			continue
		}
		sums[index] += value(point)
		counts[index]++
	}
	result := make([]float64, count)
	for i := range result {
		if counts[i] > 0 {
			result[i] = round2(sums[i] / float64(counts[i]))
		}
	}
	return result
}

// round2 保留两位小数。
func round2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}
