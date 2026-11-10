package k8s

import (
	"context"
	"log"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// 工作区相关常量
const (
	// WorkspacePath 是容器内的工作区目录：PVC 挂载到这里，
	// 云 IDE 的文件树与 Web 终端都基于该目录，保证二者看到同一份数据。
	WorkspacePath = "/home/workspace"
	// dataVolumeName 是数据盘 Volume 名称，与 PVC（<name>-data）一一对应。
	dataVolumeName = "data"
	// SharedDirName 是工作区内的共享目录名。
	SharedDirName = "shared"

	// SharedPath 是全局共享目录在容器内的挂载点，所有用户 Pod 挂载同一个 PVC。
	SharedPath = "/shared"
	// sharedVolumeName 是全局共享 Volume 在 Pod 模板中的名称。
	sharedVolumeName = "shared-data"
	// SharedPVCName 是全局共享 PVC 名称，由后端自动创建。
	SharedPVCName = "ssedeploy-shared"
	// sharedPVCSize 是全局共享 PVC 的默认容量。
	sharedPVCSize = "10Gi"
)

// mainContainerIndex 返回主容器在容器列表中的下标，找不到时回退到 0。
func mainContainerIndex(containers []corev1.Container) int {
	for i := range containers {
		if containers[i].Name == mainContainer {
			return i
		}
	}
	return 0
}

// hasVolume 判断 Pod 模板中是否已经声明了指定名称的 Volume。
func hasVolume(volumes []corev1.Volume, name string) bool {
	for i := range volumes {
		if volumes[i].Name == name {
			return true
		}
	}
	return false
}

// hasMount 判断容器是否已经把 Volume 挂载到指定路径。
func hasMount(container *corev1.Container, path string) bool {
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].MountPath == path {
			return true
		}
	}
	return false
}

// ensureSharedPVC 保证全局共享 PVC 存在（幂等）。
// 所有用户 Pod 都挂载它，从而看到同一份 /shared 数据。
func (s *PodService) ensureSharedPVC(ctx context.Context) error {
	_, err := s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).Get(ctx, SharedPVCName, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	size, err := resource.ParseQuantity(sharedPVCSize)
	if err != nil {
		return err
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SharedPVCName,
			Namespace: s.namespace,
			Labels:    map[string]string{ManagedLabel: "true"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
		},
	}
	if s.client.StorageClass != "" {
		sc := s.client.StorageClass
		pvc.Spec.StorageClassName = &sc
	}
	if _, err := s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// RepairWorkspaces 修复历史 Pod：把数据盘补挂到 /home/workspace，并设置工作目录。
//
// 背景：旧版本只把 PVC 挂到 /data，导致 Web 终端进入的是容器临时文件系统，
// 与 IDE 文件树的工作区不是同一份数据。这里在启动时做一次幂等修复，
// 模板发生变化会自动触发 Deployment 滚动更新，无需手工删除 Pod。
func (s *PodService) RepairWorkspaces(ctx context.Context) (int, error) {
	deps, err := s.client.Kube.AppsV1().Deployments(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ManagedLabel + "=true",
	})
	if err != nil {
		return 0, err
	}

	// 共享 PVC 必须先存在，否则补挂 /shared 会让 Pod 卡在 Pending
	sharedReady := true
	if err := s.ensureSharedPVC(ctx); err != nil {
		log.Printf("初始化共享存储失败，跳过 /shared 挂载修复: %v", err)
		sharedReady = false
	}

	repaired := 0
	for i := range deps.Items {
		dep := &deps.Items[i]
		if len(dep.Spec.Template.Spec.Containers) == 0 {
			continue
		}

		changed := false
		// 0. RWO 数据盘必须使用 Recreate，否则滚动更新会因存储被占用而卡住
		if dep.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
			dep.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}
			changed = true
		}

		// 1. 确保数据盘 Volume 存在（引用该 Pod 自己的 PVC）
		if !hasVolume(dep.Spec.Template.Spec.Volumes, dataVolumeName) {
			dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: dataVolumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: pvcName(dep.Name),
					},
				},
			})
			changed = true
		}

		// 2. 主容器补挂工作区目录，并设置默认工作目录
		container := &dep.Spec.Template.Spec.Containers[mainContainerIndex(dep.Spec.Template.Spec.Containers)]
		if !hasMount(container, WorkspacePath) {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      dataVolumeName,
				MountPath: WorkspacePath,
			})
			changed = true
		}
		if container.WorkingDir != WorkspacePath {
			container.WorkingDir = WorkspacePath
			changed = true
		}

		// 3. 补挂全局共享目录 /shared（所有 Pod 共用同一个 PVC）
		if sharedReady && !hasVolume(dep.Spec.Template.Spec.Volumes, sharedVolumeName) {
			dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: sharedVolumeName,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: SharedPVCName,
					},
				},
			})
			changed = true
		}
		if sharedReady && !hasMount(container, SharedPath) {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name:      sharedVolumeName,
				MountPath: SharedPath,
			})
			changed = true
		}

		if !changed {
			continue
		}
		if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
			log.Printf("修复 Pod 工作区失败：%s: %v", dep.Name, err)
			continue
		}
		repaired++
	}
	return repaired, nil
}
