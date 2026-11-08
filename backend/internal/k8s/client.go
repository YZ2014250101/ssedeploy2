// Package k8s 封装 client-go，全部 Kubernetes 操作都在这一层完成。
package k8s

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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

// defaultKubeconfigPath 返回默认 kubeconfig 路径（遵循 KUBECONFIG 环境变量）。
func defaultKubeconfigPath() string {
	if v := os.Getenv("KUBECONFIG"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
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
		path := cfg.Kubeconfig
		if path == "" {
			path = defaultKubeconfigPath()
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
