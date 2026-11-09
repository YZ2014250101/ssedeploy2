package k8s

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
)

const (
	// gpuSampleInterval 与主机页保持一致，每 3 秒采样一次
	gpuSampleInterval = 3 * time.Second
	// gpuMaxSamples 对应页面上的「近120采样点」
	gpuMaxSamples = 120
)

// gpuHistory 保存单块 GPU 的利用率与显存时序。
type gpuHistory struct {
	util []model.GPUTrendPoint
	mem  []model.GPUTrendPoint
}

// gpuDevice 是 nvidia-smi 一次查询得到的原始指标。
type gpuDevice struct {
	id       int
	name     string
	uuid     string
	util     float64
	memUsed  uint64
	memTotal uint64
	temp     int
	power    float64
	powerMax float64
	driver   string
}

// podGPUAssignment 是一个申请了 GPU 的 Pod 及其卡分配。
type podGPUAssignment struct {
	name      string
	namespace string
	uid       string
	gpuCount  int
	gpuIDs    []int            // 由 NVIDIA_VISIBLE_DEVICES 解析出的显式卡号
	memByGPU  map[int]uint64   // 进程级显存占用（按卡号）
}

// GPUService 提供 GPU 监控数据：nvidia-smi 采集 + K3s Pod 归属匹配。
type GPUService struct {
	client *Client

	mu            sync.Mutex
	history       map[int]*gpuHistory
	driverVersion string
}

// NewGPUService 创建 GPU 监控服务。
func NewGPUService(client *Client) *GPUService {
	return &GPUService{client: client, history: map[int]*gpuHistory{}}
}

// Start 启动后台采样，每 3 秒写入一次利用率与显存时序。
func (s *GPUService) Start(ctx context.Context) {
	go func() {
		s.sample()
		ticker := time.NewTicker(gpuSampleInterval)
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

// sample 采集一次 nvidia-smi 并追加时序点。
func (s *GPUService) sample() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	devices := readGPUDevices(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Unix()
	for _, device := range devices {
		history := s.history[device.id]
		if history == nil {
			history = &gpuHistory{}
			s.history[device.id] = history
		}
		history.util = appendPoint(history.util, model.GPUTrendPoint{TS: now, Value: device.util})
		history.mem = appendPoint(history.mem, model.GPUTrendPoint{TS: now, Value: float64(device.memUsed)})
		if device.driver != "" {
			s.driverVersion = device.driver
		}
	}
}

// appendPoint 追加采样点并裁剪到上限。
func appendPoint(points []model.GPUTrendPoint, point model.GPUTrendPoint) []model.GPUTrendPoint {
	points = append(points, point)
	if len(points) > gpuMaxSamples {
		points = points[len(points)-gpuMaxSamples:]
	}
	return points
}

// Get 返回 GPU 监控页的完整数据。
func (s *GPUService) Get(ctx context.Context) (*model.GPUOverview, error) {
	devices := readGPUDevices(ctx)

	overview := &model.GPUOverview{
		CollectedAt: time.Now().Unix(),
		Summary:     model.GPUPageSummary{DriverVersion: s.driver()},
		GPUList:     make([]model.GPUDevice, 0, len(devices)),
		Trend:       model.GPUTrend{MaxPoints: gpuMaxSamples, Series: []model.GPUTrendSeries{}},
		PodAlloc:    make([]model.GPUPodAlloc, 0, len(devices)),
	}

	// 1. 汇总卡片 + 每卡实时指标
	var utilSum float64
	for _, device := range devices {
		overview.GPUList = append(overview.GPUList, model.GPUDevice{
			ID:       device.id,
			Name:     device.name,
			Temp:     device.temp,
			Power:    device.power,
			PowerMax: device.powerMax,
			Util:     device.util,
			MemUsed:  device.memUsed,
			MemTotal: device.memTotal,
		})
		utilSum += device.util
		overview.Summary.MemUsed += device.memUsed
		overview.Summary.MemTotal += device.memTotal
		if device.temp > overview.Summary.MaxTemp {
			overview.Summary.MaxTemp = device.temp
		}
		if device.driver != "" {
			overview.Summary.DriverVersion = device.driver
		}
	}
	overview.Summary.Count = len(devices)
	if len(devices) > 0 {
		overview.Summary.AvgUtil = utilSum / float64(len(devices))
	}

	// 2. 时序：从环形缓存复制，避免与采样 goroutine 竞争
	overview.Trend.Series = s.snapshotTrend(devices)

	// 3. Pod 归属：仅本机节点，按 nvidia.com/gpu 申请与 NVIDIA_VISIBLE_DEVICES 匹配
	assignments := s.collectPodAssignments(ctx, devices)
	overview.PodAlloc = buildPodAlloc(devices, assignments)

	return overview, nil
}

// driver 返回缓存中的驱动版本。
func (s *GPUService) driver() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.driverVersion
}

// snapshotTrend 返回每块 GPU 的时序副本。
func (s *GPUService) snapshotTrend(devices []gpuDevice) []model.GPUTrendSeries {
	s.mu.Lock()
	defer s.mu.Unlock()

	series := make([]model.GPUTrendSeries, 0, len(devices))
	for _, device := range devices {
		item := model.GPUTrendSeries{GPUId: device.id, Util: []model.GPUTrendPoint{}, Mem: []model.GPUTrendPoint{}}
		if history := s.history[device.id]; history != nil {
			item.Util = append(item.Util, history.util...)
			item.Mem = append(item.Mem, history.mem...)
		}
		series = append(series, item)
	}
	return series
}

// readGPUDevices 执行 nvidia-smi 查询所有 GPU 的实时指标。
func readGPUDevices(ctx context.Context) []gpuDevice {
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw,power.limit,driver_version,uuid",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return []gpuDevice{}
	}

	devices := make([]gpuDevice, 0, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 10 {
			continue
		}
		devices = append(devices, gpuDevice{
			id:       atoi(strings.TrimSpace(fields[0])),
			name:     strings.TrimSpace(fields[1]),
			util:     parseFloatSafe(fields[2]),
			memUsed:  uint64(parseFloatSafe(fields[3])) * 1024 * 1024,
			memTotal: uint64(parseFloatSafe(fields[4])) * 1024 * 1024,
			temp:     atoi(strings.TrimSpace(fields[5])),
			power:    parseFloatSafe(fields[6]),
			powerMax: parseFloatSafe(fields[7]),
			driver:   strings.TrimSpace(fields[8]),
			uuid:     strings.TrimSpace(fields[9]),
		})
	}
	return devices
}

// collectPodAssignments 找出节点上申请了 GPU 的 Pod，并解析卡归属与进程占用的显存。
func (s *GPUService) collectPodAssignments(ctx context.Context, devices []gpuDevice) []podGPUAssignment {
	if len(devices) == 0 {
		return nil
	}
	node, err := s.selfNode(ctx)
	if err != nil || node == "" {
		return nil
	}

	pods, err := s.client.Kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + node,
	})
	if err != nil {
		return nil
	}

	// 1. 过滤申请了 GPU 的 Pod
	assignments := make([]podGPUAssignment, 0, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		count := gpuRequestCount(pod)
		if count <= 0 {
			continue
		}
		assignments = append(assignments, podGPUAssignment{
			name:      pod.Name,
			namespace: pod.Namespace,
			uid:       string(pod.UID),
			gpuCount:  count,
			gpuIDs:    visibleGPUIds(pod, devices),
			memByGPU:  map[int]uint64{},
		})
	}
	if len(assignments) == 0 {
		return nil
	}

	// 2. 进程级显存：nvidia-smi compute-apps + /proc/<pid>/cgroup 反查 Pod
	indexByUUID := map[string]int{}
	uidSet := map[string]int{}
	for _, device := range devices {
		indexByUUID[device.uuid] = device.id
	}
	for i := range assignments {
		uidSet[assignments[i].uid] = i
	}
	for _, app := range readComputeApps(ctx) {
		gpuID, ok := indexByUUID[app.uuid]
		if !ok {
			continue
		}
		podIndex, ok := uidSet[podUIDForPID(app.pid)]
		if !ok {
			continue
		}
		assignments[podIndex].memByGPU[gpuID] += app.memBytes
	}

	// 3. 没有显式卡号的 Pod 按当前负载最少的卡依次分配
	sort.Slice(assignments, func(i, j int) bool {
		if assignments[i].namespace != assignments[j].namespace {
			return assignments[i].namespace < assignments[j].namespace
		}
		return assignments[i].name < assignments[j].name
	})
	load := map[int]int{}
	for i := range assignments {
		for _, gpuID := range assignments[i].gpuIDs {
			load[gpuID]++
		}
	}
	for i := range assignments {
		if len(assignments[i].gpuIDs) > 0 {
			continue
		}
		for n := 0; n < assignments[i].gpuCount; n++ {
			gpuID := leastLoadedGPU(devices, load)
			if gpuID < 0 {
				break
			}
			assignments[i].gpuIDs = append(assignments[i].gpuIDs, gpuID)
			load[gpuID]++
		}
	}
	return assignments
}

// selfNode 返回采集器所在节点。
func (s *GPUService) selfNode(ctx context.Context) (string, error) {
	nodes, err := s.client.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("读取节点失败: %w", err)
	}
	return resolveSelfNode(nodes.Items), nil
}

// leastLoadedGPU 返回当前分配 Pod 数最少的 GPU 编号。
func leastLoadedGPU(devices []gpuDevice, load map[int]int) int {
	best := -1
	for _, device := range devices {
		if best == -1 || load[device.id] < load[best] {
			best = device.id
		}
	}
	return best
}

// gpuRequestCount 统计 Pod 声明的 nvidia.com/gpu 数量。
func gpuRequestCount(pod *corev1.Pod) int {
	const resourceName corev1.ResourceName = "nvidia.com/gpu"
	total := 0
	for _, container := range pod.Spec.Containers {
		if quantity, ok := container.Resources.Limits[resourceName]; ok {
			total += int(quantity.Value())
			continue
		}
		if quantity, ok := container.Resources.Requests[resourceName]; ok {
			total += int(quantity.Value())
		}
	}
	// 兼容以 annotation 声明卡数的场景
	if total == 0 {
		if value, ok := pod.Annotations["nvidia.com/gpu"]; ok {
			if count, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
				total = count
			}
		}
	}
	return total
}

// visibleGPUIds 从 NVIDIA_VISIBLE_DEVICES 解析卡号（支持 UUID 或数字）。
func visibleGPUIds(pod *corev1.Pod, devices []gpuDevice) []int {
	ids := make([]int, 0, 2)
	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name != "NVIDIA_VISIBLE_DEVICES" || env.Value == "" || env.Value == "all" || env.Value == "void" {
				continue
			}
			for _, token := range strings.Split(env.Value, ",") {
				token = strings.TrimSpace(token)
				for _, device := range devices {
					if token == device.uuid || token == strconv.Itoa(device.id) {
						ids = append(ids, device.id)
					}
				}
			}
		}
	}
	return uniqueInts(ids)
}

// uniqueInts 去重。
func uniqueInts(values []int) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// buildPodAlloc 按卡汇总 Pod 占用；没有进程级数据时按已用显存均分。
func buildPodAlloc(devices []gpuDevice, assignments []podGPUAssignment) []model.GPUPodAlloc {
	result := make([]model.GPUPodAlloc, 0, len(devices))
	for _, device := range devices {
		allocation := model.GPUPodAlloc{GPUId: device.id, Pods: []model.PodGPUUsage{}}

		matched := make([]*podGPUAssignment, 0, 2)
		var mappedTotal uint64
		for i := range assignments {
			for _, gpuID := range assignments[i].gpuIDs {
				if gpuID != device.id {
					continue
				}
				matched = append(matched, &assignments[i])
				mappedTotal += assignments[i].memByGPU[device.id]
				break
			}
		}
		if len(matched) == 0 {
			result = append(result, allocation)
			continue
		}

		// 没有进程级显存时按 Pod 数均分该卡已用显存
		fallbackMem := uint64(0)
		if mappedTotal == 0 {
			fallbackMem = device.memUsed / uint64(len(matched))
		}
		for _, assignment := range matched {
			memUsed := assignment.memByGPU[device.id]
			if memUsed == 0 {
				memUsed = fallbackMem
			}
			percent := 0.0
			if device.memTotal > 0 {
				percent = clampPercent(float64(memUsed) / float64(device.memTotal) * 100)
			}
			allocation.Pods = append(allocation.Pods, model.PodGPUUsage{
				Name:      assignment.name,
				Namespace: assignment.namespace,
				MemUsed:   memUsed,
				Percent:   percent,
				Util:      clampPercent(device.util / float64(len(matched))),
			})
		}
		result = append(result, allocation)
	}
	return result
}

// computeApp 是 nvidia-smi compute-apps 的一行。
type computeApp struct {
	uuid     string
	pid      int
	memBytes uint64
}

// readComputeApps 读取每个 GPU 进程占用的显存。
func readComputeApps(ctx context.Context) []computeApp {
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-compute-apps=gpu_uuid,pid,used_memory",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	apps := make([]computeApp, 0, 4)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 3 {
			continue
		}
		apps = append(apps, computeApp{
			uuid:     strings.TrimSpace(fields[0]),
			pid:      atoi(strings.TrimSpace(fields[1])),
			memBytes: uint64(parseFloatSafe(fields[2])) * 1024 * 1024,
		})
	}
	return apps
}

// parseFloatSafe 解析浮点数，nvidia-smi 的 [N/A] 等非法值返回 0。
func parseFloatSafe(value string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return v
}
