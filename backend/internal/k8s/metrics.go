package k8s

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PodUsage 从 metrics-server 汇总该 Pod 下所有实例的实时用量。
// 返回 CPU 毫核与内存字节数；metrics-server 未安装时返回错误。
func (s *PodService) PodUsage(ctx context.Context, name string) (int64, int64, error) {
	list, err := s.client.Metrics.MetricsV1beta1().PodMetricses(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: appLabel + "=" + name + "," + ManagedLabel + "=true",
	})
	if err != nil {
		return 0, 0, err
	}

	var cpuMilli, memBytes int64
	for _, pm := range list.Items {
		for _, c := range pm.Containers {
			cpuMilli += c.Usage.Cpu().MilliValue()
			memBytes += c.Usage.Memory().Value()
		}
	}
	return cpuMilli, memBytes, nil
}

// Limits 返回 Deployment 中声明的 CPU（毫核）与内存（字节）上限。
func (s *PodService) Limits(ctx context.Context, name string) (int64, int64, error) {
	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	limits := containerOf(dep).Resources.Limits
	var cpuMilli, memBytes int64
	if q, ok := limits[corev1.ResourceCPU]; ok {
		cpuMilli = q.MilliValue()
	}
	if q, ok := limits[corev1.ResourceMemory]; ok {
		memBytes = q.Value()
	}
	return cpuMilli, memBytes, nil
}

// SampleAll 对所有平台 Pod 采样一次并写入数据库，由后台定时任务调用。
// 单个 Pod 采样失败不影响其他 Pod。
func (s *PodService) SampleAll(ctx context.Context) {
	// 每次采样最多等待 10 秒，避免 apiserver 异常时任务堆积
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	deps, err := s.client.Kube.AppsV1().Deployments(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ManagedLabel + "=true",
	})
	if err != nil {
		return
	}

	now := time.Now().Unix()
	for _, dep := range deps.Items {
		cpuMilli, memBytes, err := s.PodUsage(ctx, dep.Name)
		if err != nil {
			continue // metrics-server 不可用时跳过，接口层会返回 available=false
		}
		_ = s.store.InsertMetric(s.namespace, dep.Name, cpuMilli, memBytes, now)
	}
}
