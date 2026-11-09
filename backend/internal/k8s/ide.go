package k8s

import (
	"context"
	"errors"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

// ErrPodForbidden 表示当前用户不是该 Pod 的负责人。
// Handler 层会把它映射为 HTTP 403，终端与日志 WebSocket 在升级前直接拒绝。
var ErrPodForbidden = errors.New("无权访问该 Pod")

// ListOwned 返回当前用户自己创建的 Pod，供 IDE 下拉选择器使用。
// 拥有关系优先取数据库元数据，缺失时回退到 Deployment 的 owner 注解。
func (s *PodService) ListOwned(ctx context.Context, userID int64) ([]model.IDEPod, error) {
	deps, err := s.client.Kube.AppsV1().Deployments(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ManagedLabel + "=true",
	})
	if err != nil {
		return nil, err
	}
	pods, err := s.client.Kube.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ManagedLabel + "=true",
	})
	if err != nil {
		return nil, err
	}
	metas, err := s.store.ListPodMetas(s.namespace)
	if err != nil {
		return nil, err
	}
	metaByName := make(map[string]*model.PodMeta, len(metas))
	for _, m := range metas {
		metaByName[m.Name] = m
	}

	// 按 app 标签把真实 Pod 归到 Deployment 下
	podsByApp := make(map[string][]corev1.Pod)
	for i := range pods.Items {
		p := pods.Items[i]
		podsByApp[p.Labels[appLabel]] = append(podsByApp[p.Labels[appLabel]], p)
	}

	result := make([]model.IDEPod, 0, len(deps.Items))
	for i := range deps.Items {
		dep := &deps.Items[i]

		// 判断归属：优先数据库，缺失时读注解
		ownerID := int64(0)
		if meta := metaByName[dep.Name]; meta != nil {
			ownerID = meta.OwnerID
		} else if raw := dep.Annotations["ssedeploy.io/owner-id"]; raw != "" {
			ownerID, _ = strconv.ParseInt(raw, 10, 64)
		}
		if ownerID != userID {
			continue
		}

		code := calcStatus(dep, podsByApp[dep.Name])
		podName := ""
		if code == model.StatusRunning {
			podName = readyPodName(podsByApp[dep.Name])
		}
		result = append(result, model.IDEPod{
			Name:       dep.Name,
			Status:     code,
			StatusText: model.StatusText(code),
			PodName:    podName,
			Running:    code == model.StatusRunning && podName != "",
		})
	}
	return result, nil
}

// EnsureOwner 校验当前用户是否拥有该 Pod。
// 终端与日志接口在建立 WebSocket / 读取流之前调用。
func (s *PodService) EnsureOwner(ctx context.Context, name string, userID int64) error {
	meta, err := s.store.GetPodMeta(s.namespace, name)
	if err == nil && meta != nil {
		if meta.OwnerID != userID {
			return ErrPodForbidden
		}
		return nil
	}

	// 元数据缺失（历史数据）时回退到 Deployment 注解
	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return store.ErrNotFound
		}
		return err
	}
	ownerID, parseErr := strconv.ParseInt(dep.Annotations["ssedeploy.io/owner-id"], 10, 64)
	if parseErr != nil || ownerID != userID {
		return ErrPodForbidden
	}
	return nil
}

// readyPodName 选出优先 Ready 的运行中 Pod 名称；没有则返回空串。
func readyPodName(pods []corev1.Pod) string {
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning && podReady(&pods[i]) {
			return pods[i].Name
		}
	}
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			return pods[i].Name
		}
	}
	return ""
}
