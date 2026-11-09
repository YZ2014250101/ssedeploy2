package k8s

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
)

// OverviewService 负责聚合概览页所需的全部数据。
type OverviewService struct {
	client *Client
}

// NewOverviewService 创建概览聚合服务。
func NewOverviewService(client *Client) *OverviewService {
	return &OverviewService{client: client}
}

// systemNamespaces 是不计入「分组」的系统命名空间。
var systemNamespaces = map[string]bool{
	"default":         true,
	"kube-system":     true,
	"kube-public":     true,
	"kube-node-lease": true,
}

// Get 聚合节点、Pod、宿主机资源与 GPU 数据。
// 单个数据源失败不影响整体返回，只把对应字段置为不可用。
func (s *OverviewService) Get(ctx context.Context) (*model.Overview, error) {
	// 1. 节点：数量、Ready 数、K3s 版本
	nodes, nodesErr := s.client.Kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if nodesErr != nil {
		return nil, fmt.Errorf("读取节点失败: %w", nodesErr)
	}

	overview := &model.Overview{
		UpdatedAt: time.Now().Unix(),
		GPU:       model.GPUSummary{Items: []model.GPUCard{}},
	}

	readyNodes := 0
	for i := range nodes.Items {
		if nodeReady(&nodes.Items[i]) {
			readyNodes++
		}
	}
	overview.Cluster.NodeCount = len(nodes.Items)
	overview.Cluster.ReadyNodes = readyNodes
	overview.Cluster.Version = ""
	if len(nodes.Items) > 0 {
		// K3s 的版本信息体现在节点的 Kubelet 版本上，例如 v1.35.5+k3s1
		overview.Cluster.Version = nodes.Items[0].Status.NodeInfo.KubeletVersion
	}

	// 2. Pod：运行 / 总数 / 已停止
	pods := s.listPods(ctx)
	running, stopped := 0, 0
	for i := range pods {
		switch pods[i].Status.Phase {
		case corev1.PodRunning:
			running++
		case corev1.PodSucceeded, corev1.PodFailed:
			stopped++
		}
	}
	overview.Pods = model.PodSummary{Running: running, Total: len(pods), Stopped: stopped}
	overview.Cluster.RunningPods = running
	overview.Cluster.TotalPods = len(pods)
	overview.Cluster.StoppedPods = stopped

	// 3. 分组：业务命名空间数量（排除系统命名空间）
	overview.Cluster.Groups = s.countGroups(ctx)

	// 4. 开发 Agent：平台命名空间内处于 Running 的 Pod 数
	agents := 0
	for i := range pods {
		if pods[i].Namespace == s.client.Namespace && pods[i].Status.Phase == corev1.PodRunning {
			agents++
		}
	}
	overview.Modules = model.ModuleStat{
		Nodes:  overview.Cluster.NodeCount,
		// 节点全部 Ready 才显示绿色在线圆点
		NodesOnline:  overview.Cluster.NodeCount > 0 && readyNodes == overview.Cluster.NodeCount,
		Agents:       agents,
		AgentsOnline: agents > 0,
		// 攻防数据属于后续阶段，先固定为 0，前端展示红色告警圆点
		Attacks: 0,
		Bans:    0,
	}

	// 5. 宿主机 CPU / 内存 / 磁盘（读取 /proc 与 statfs，非 Linux 时返回不可用）
	overview.CPU, overview.Memory, overview.Disk = readHostStats()

	// 6. GPU：调用本机 nvidia-smi，未安装或无 GPU 时返回空列表
	overview.GPU = readGPU(ctx)

	return overview, nil
}

// listPods 一次性拉取集群内全部 Pod；没有集群级权限时退回业务命名空间。
func (s *OverviewService) listPods(ctx context.Context) []corev1.Pod {
	all, err := s.client.Kube.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err == nil {
		return all.Items
	}
	// 退回命名空间级读取，保证概览页仍可展示平台自身数据
	scoped, err := s.client.Kube.CoreV1().Pods(s.client.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}
	return scoped.Items
}

// countGroups 统计业务命名空间数量，失败时返回 0。
func (s *OverviewService) countGroups(ctx context.Context) int {
	list, err := s.client.Kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0
	}
	count := 0
	for i := range list.Items {
		name := list.Items[i].Name
		// 系统命名空间与以 kube- 开头的命名空间不计入分组
		if systemNamespaces[name] || strings.HasPrefix(name, "kube-") {
			continue
		}
		count++
	}
	return count
}

// nodeReady 判断节点 Ready 条件是否为 True。
func nodeReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// readGPU 调用 nvidia-smi 读取每张 GPU 的利用率、显存与温度。
func readGPU(parent context.Context) model.GPUSummary {
	result := model.GPUSummary{Items: []model.GPUCard{}}

	// 单次查询限制 3 秒，避免 nvidia-smi 卡住拖慢接口
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	// noheader：不输出表头；nounits：数值不带单位，方便直接解析
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits")
	out, err := cmd.Output()
	if err != nil {
		// nvidia-smi 不存在或没有 GPU：视为无 GPU 环境
		return result
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) < 6 {
			continue
		}
		card := model.GPUCard{
			Index:       atoi(strings.TrimSpace(fields[0])),
			Name:        strings.TrimSpace(fields[1]),
			Utilization: atoi(strings.TrimSpace(fields[2])),
			// nvidia-smi 显存单位为 MiB，统一换算成字节
			MemoryUsed:  int64(atoi(strings.TrimSpace(fields[3]))) * 1024 * 1024,
			MemoryTotal: int64(atoi(strings.TrimSpace(fields[4]))) * 1024 * 1024,
			Temperature: atoi(strings.TrimSpace(fields[5])),
		}
		result.Items = append(result.Items, card)
	}
	result.Count = len(result.Items)
	return result
}

// atoi 把字符串转成整数，解析失败返回 0。
func atoi(raw string) int {
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return v
}

// percentOf 计算百分比并保留两位小数。
func percentOf(used, total int64) float64 {
	if total <= 0 {
		return 0
	}
	value := float64(used) / float64(total) * 100
	return float64(int(value*100+0.5)) / 100
}
