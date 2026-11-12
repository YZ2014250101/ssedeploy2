// Package k8s 封装 client-go，全部 Kubernetes 操作都在这一层完成。
package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"

	"ssedeploy/internal/config"
)

// Client 持有 K8s 与 metrics-server 的客户端。
type Client struct {
	Kube         kubernetes.Interface
	Metrics      metricsclient.Interface
	Config       *rest.Config
	Namespace    string // 业务命名空间
	StorageClass string // PVC StorageClass，空表示使用集群默认
}

// kubeconfigCandidates 按优先级返回候选 kubeconfig 路径：
// 1. SSD_KUBECONFIG；2. KUBECONFIG（支持多路径）；3. ~/.kube/config；
// 4. 当前目录 / 上级目录（本地 go run 调试时把 kubeconfig 放在仓库根目录即可）。
func kubeconfigCandidates(cfg *config.Config) []string {
	var paths []string
	if cfg.Kubeconfig != "" {
		paths = append(paths, cfg.Kubeconfig)
	}
	for _, p := range filepath.SplitList(os.Getenv("KUBECONFIG")) {
		if p != "" {
			paths = append(paths, p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".kube", "config"))
	}
	paths = append(paths, "kubeconfig", filepath.Join("..", "kubeconfig"))
	return paths
}

// resolveKubeconfig 返回第一个存在且非空的 kubeconfig 路径。
// 空文件（例如某些工具生成的占位文件）会被跳过，避免 clientcmd 报
// "invalid configuration: no configuration has been provided"。
func resolveKubeconfig(cfg *config.Config) (string, []string) {
	var tried []string
	for _, p := range kubeconfigCandidates(cfg) {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() || info.Size() == 0 {
			tried = append(tried, p)
			continue
		}
		return p, tried
	}
	return "", tried
}

// New 初始化 K8s 客户端：优先使用集群内 ServiceAccount，其次使用 kubeconfig。
func New(cfg *config.Config) (*Client, error) {
	var (
		restCfg *rest.Config
		err     error
	)

	if os.Getenv("KUBERNETES_SERVICE_HOST") != "" {
		// 集群内运行（Pod 中挂载 ServiceAccount）
		restCfg, err = rest.InClusterConfig()
	} else {
		path, tried := resolveKubeconfig(cfg)
		if path == "" {
			return nil, fmt.Errorf(
				"加载 K8s 配置失败: 未找到可用的 kubeconfig（已尝试: %s）\n"+
					"本地调试请先把内嵌 K3s 的 kubeconfig 导出到仓库根目录:\n"+
					"  docker cp ssedeploy-k3s:/etc/rancher/k3s/k3s.yaml ./kubeconfig\n"+
					"或用环境变量指定: SSD_KUBECONFIG=<绝对路径>",
				strings.Join(tried, ", "))
		}
		restCfg, err = clientcmd.BuildConfigFromFlags("", path)
	}
	if err != nil {
		return nil, fmt.Errorf("加载 K8s 配置失败: %w", err)
	}

	// 提升限流阈值，列表页与指标采样会并发访问 apiserver
	restCfg.QPS = 50
	restCfg.Burst = 100

	kube, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("创建 K8s 客户端失败: %w", err)
	}

	metrics, err := metricsclient.NewForConfig(restCfg)
	if err != nil {
		return nil, fmt.Errorf("创建 metrics 客户端失败: %w", err)
	}

	client := &Client{
		Kube:         kube,
		Metrics:      metrics,
		Config:       restCfg,
		Namespace:    cfg.Namespace,
		StorageClass: cfg.StorageClass,
	}

	// 业务命名空间不存在时自动创建，降低部署门槛
	client.ensureNamespace(context.Background())

	return client, nil
}

// ensureNamespace 幂等地创建业务命名空间。
func (c *Client) ensureNamespace(ctx context.Context) {
	_, err := c.Kube.CoreV1().Namespaces().Get(ctx, c.Namespace, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		return
	}
	_, _ = c.Kube.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: c.Namespace},
	}, metav1.CreateOptions{})
}
