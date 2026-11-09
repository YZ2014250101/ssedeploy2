package k8s

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

// hostSampleInterval 是主机指标采样间隔，与前端 3 秒轮询保持一致。
const hostSampleInterval = 3 * time.Second

// HostService 提供主机健康详情数据。
// 指标由本机采集器采样（后端所在节点），集群信息由 client-go 读取。
type HostService struct {
	client  *Client
	sampler *hostSampler
}

// NewHostService 创建主机详情服务。
func NewHostService(client *Client) *HostService {
	return &HostService{client: client, sampler: &hostSampler{}}
}

// Start 启动后台采样，每 3 秒采集一次并保留最近 120 个采样点。
func (s *HostService) Start(ctx context.Context) {
	s.sampler.start(ctx, hostSampleInterval)
}

// NodeHealth 实时读取 /proc/loadavg，返回 1/5/15 分钟平均负载。
func (s *HostService) NodeHealth() model.NodeHealth {
	return ReadNodeHealth()
}

// Get 返回指定节点的主机健康详情；nodeName 传 self 表示后端所在节点。
func (s *HostService) Get(ctx context.Context, nodeName string) (*model.HostDetail, error) {
	nodes, err := s.client.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("读取节点失败: %w", err)
	}
	if len(nodes.Items) == 0 {
		return nil, store.ErrNotFound
	}

	// 采集器所在节点：主机名匹配优先，单节点集群直接取唯一节点
	selfNode := resolveSelfNode(nodes.Items)

	target := nodeName
	if target == "" || target == "self" || target == "local" {
		target = selfNode
	}
	var node *corev1.Node
	for i := range nodes.Items {
		if nodes.Items[i].Name == target {
			node = &nodes.Items[i]
			break
		}
	}
	if node == nil {
		return nil, store.ErrNotFound
	}

	detail := &model.HostDetail{
		CollectedAt: time.Now().Unix(),
		Series:      model.HostSeries{MaxPoints: maxSamples, Samples: []model.MetricSample{}},
		Partitions:  []model.DiskPartition{},
		Nodes:       make([]model.NodeBrief, 0, len(nodes.Items)),
		GPU:         model.HostGPU{Items: []model.GPUCard{}},
	}

	// 1. 系统信息：来自 Kubernetes Node Status
	detail.Node = model.HostBasic{
		Name:          node.Name,
		Hostname:      node.Labels["kubernetes.io/hostname"],
		OSImage:       node.Status.NodeInfo.OSImage,
		KernelVersion: node.Status.NodeInfo.KernelVersion,
		Architecture:  node.Status.NodeInfo.Architecture,
		CPUCores:      int(node.Status.Capacity.Cpu().Value()),
		K3sVersion:    node.Status.NodeInfo.KubeletVersion,
	}
	if detail.Node.Hostname == "" {
		detail.Node.Hostname = node.Name
	}

	// 2. 该节点上运行的 Pod 数
	pods, err := s.client.Kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + target,
	})
	if err == nil {
		running := 0
		for i := range pods.Items {
			if pods.Items[i].Status.Phase == corev1.PodRunning {
				running++
			}
		}
		detail.Node.PodCount = running
	}

	// 3. 节点列表（含三种压力状态）
	for i := range nodes.Items {
		n := &nodes.Items[i]
		detail.Nodes = append(detail.Nodes, model.NodeBrief{
			Name:           n.Name,
			Ready:          conditionTrue(n, corev1.NodeReady),
			DiskPressure:   conditionTrue(n, corev1.NodeDiskPressure),
			MemoryPressure: conditionTrue(n, corev1.NodeMemoryPressure),
			PIDPressure:    conditionTrue(n, corev1.NodePIDPressure),
			Version:        n.Status.NodeInfo.KubeletVersion,
		})
	}

	// 4. 本机采集数据：只有目标节点就是采集器所在节点时才有值
	latest, raw, samples, available := s.sampler.snapshot()
	detail.Realtime.CollectorNode = selfNode
	isSelf := target == selfNode && available
	detail.Realtime.Available = isSelf

	if isSelf {
		detail.Series.Samples = samples
		detail.Partitions = raw.partitions
		if detail.Partitions == nil {
			detail.Partitions = []model.DiskPartition{}
		}
		detail.Node.UptimeSeconds = raw.uptime
		detail.Node.Load1 = raw.load1
		detail.Node.Load5 = raw.load5
		detail.Node.Load15 = raw.load15
		detail.Node.SwapTotal = raw.swapTotal
		if raw.swapTotal > raw.swapFree {
			detail.Node.SwapUsedBytes = raw.swapTotal - raw.swapFree
		}

		detail.Realtime.CPUPercent = latest.CPUPercent
		detail.Realtime.MemoryPercent = latest.MemoryPercent
		detail.Realtime.MemoryUsed = raw.memTotal - raw.memAvail
		detail.Realtime.MemoryTotal = raw.memTotal
		detail.Realtime.DiskUsed = raw.rootUsed
		detail.Realtime.DiskTotal = raw.rootTotal
		if raw.rootTotal > 0 {
			detail.Realtime.DiskPercent = float64(raw.rootUsed) / float64(raw.rootTotal) * 100
		}
		detail.Realtime.NetRxBps = latest.NetRxBps
		detail.Realtime.NetTxBps = latest.NetTxBps
		detail.Realtime.DiskReadBps = latest.DiskReadBps
		detail.Realtime.DiskWriteBps = latest.DiskWriteBps
		detail.Realtime.PktRx = latest.PktRx
		detail.Realtime.PktTx = latest.PktTx
		detail.Realtime.IOPSRead = latest.IOPSRead
		detail.Realtime.IOPSWrite = latest.IOPSWrite
		detail.Realtime.RxDropPercent = latest.RxDropPercent
		detail.Realtime.TxDropPercent = latest.TxDropPercent
		detail.Realtime.DiskAwaitMs = latest.DiskAwaitMs
		detail.Realtime.DiskBusyPercent = latest.DiskBusyPercent
	}

	// 5. GPU 汇总
	detail.GPU = readHostGPU(ctx)

	return detail, nil
}

// resolveSelfNode 找出采集器所在的节点：主机名匹配优先，单节点集群直接返回唯一节点。
func resolveSelfNode(nodes []corev1.Node) string {
	hostname, _ := os.Hostname()
	for i := range nodes {
		if nodes[i].Name == hostname || nodes[i].Labels["kubernetes.io/hostname"] == hostname {
			return nodes[i].Name
		}
	}
	if len(nodes) == 1 {
		return nodes[0].Name
	}
	return ""
}

// conditionTrue 判断节点某个状态条件是否为 True。
func conditionTrue(node *corev1.Node, condType corev1.NodeConditionType) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == condType {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// readHostGPU 通过 nvidia-smi 读取 GPU 数量、驱动版本与每卡信息。
func readHostGPU(parent context.Context) model.HostGPU {
	result := model.HostGPU{Items: []model.GPUCard{}}

	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu,driver_version",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		return result
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 7 {
			continue
		}
		result.Items = append(result.Items, model.GPUCard{
			Index:       atoi(strings.TrimSpace(fields[0])),
			Name:        strings.TrimSpace(fields[1]),
			Utilization: atoi(strings.TrimSpace(fields[2])),
			MemoryUsed:  int64(atoi(strings.TrimSpace(fields[3]))) * 1024 * 1024,
			MemoryTotal: int64(atoi(strings.TrimSpace(fields[4]))) * 1024 * 1024,
			Temperature: atoi(strings.TrimSpace(fields[5])),
		})
		result.DriverVersion = strings.TrimSpace(fields[6])
	}
	result.Count = len(result.Items)
	return result
}
