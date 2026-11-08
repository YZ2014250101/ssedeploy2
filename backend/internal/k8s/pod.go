package k8s

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

const (
	// ManagedLabel 标记资源由本平台创建，列表只展示带该标签的 Deployment
	ManagedLabel = "ssedeploy.io/managed"
	// appLabel 用于把 Pod 与所属 Deployment 关联
	appLabel = "app"
	// mainContainer 是平台创建容器时固定的容器名
	mainContainer = "main"
	// RestartedAtAnnotation 触发滚动重启的标准注解
	RestartedAtAnnotation = "kubectl.kubernetes.io/restartedAt"
)

// nameRe 校验 Pod（Deployment）名称，遵循 K8s DNS-1123 规则
var nameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// PodService 是页面语义上的 "Pod"：一个 Pod 在底层对应一个 Deployment + 一个 PVC。
type PodService struct {
	client    *Client
	store     *store.Store
	namespace string
}

// NewPodService 创建 Pod 业务服务。
func NewPodService(client *Client, st *store.Store) *PodService {
	return &PodService{client: client, store: st, namespace: client.Namespace}
}

// Namespace 返回业务命名空间。
func (s *PodService) Namespace() string { return s.namespace }

// pvcName 返回 Pod 数据盘 PVC 名称。
func pvcName(name string) string { return name + "-data" }

// containerOf 从 Deployment 中取出主容器，取不到时回退到第一个容器。
func containerOf(d *appsv1.Deployment) corev1.Container {
	for _, c := range d.Spec.Template.Spec.Containers {
		if c.Name == mainContainer {
			return c
		}
	}
	if len(d.Spec.Template.Spec.Containers) > 0 {
		return d.Spec.Template.Spec.Containers[0]
	}
	return corev1.Container{}
}

// List 返回平台创建的 Pod 列表，支持关键字与状态过滤。
func (s *PodService) List(ctx context.Context, keyword, status string, userID int64) ([]model.PodView, error) {
	deps, err := s.client.Kube.AppsV1().Deployments(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ManagedLabel + "=true",
	})
	if err != nil {
		return nil, fmt.Errorf("读取 Deployment 失败: %w", err)
	}

	// 一次性拉取全部 Pod，再按 app 标签分组，避免逐个 Deployment 查询
	podList, err := s.client.Kube.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: ManagedLabel + "=true",
	})
	if err != nil {
		return nil, fmt.Errorf("读取 Pod 失败: %w", err)
	}
	podsByApp := make(map[string][]corev1.Pod)
	for i := range podList.Items {
		p := podList.Items[i]
		podsByApp[p.Labels[appLabel]] = append(podsByApp[p.Labels[appLabel]], p)
	}

	// 元数据与用户一次性载入内存，用于补充负责人与角色
	metas, err := s.store.ListPodMetas(s.namespace)
	if err != nil {
		return nil, err
	}
	metaByName := make(map[string]*model.PodMeta, len(metas))
	for _, m := range metas {
		metaByName[m.Name] = m
	}
	users, err := s.store.ListUsers()
	if err != nil {
		return nil, err
	}

	result := make([]model.PodView, 0, len(deps.Items))
	for i := range deps.Items {
		dep := &deps.Items[i]
		meta := metaByName[dep.Name]
		code := calcStatus(dep, podsByApp[dep.Name])

		view := model.PodView{
			Name:          dep.Name,
			Status:        code,
			StatusText:    model.StatusText(code),
			Spec:          specOf(meta, containerOf(dep)),
			Image:         imageOf(meta, containerOf(dep)),
			Owner:         ownerName(meta, users),
			MyRole:        myRole(meta, userID),
			Replicas:      replicasOf(dep),
			ReadyReplicas: dep.Status.ReadyReplicas,
			CreatedAt:     createdAtOf(meta, dep),
		}

		// 关键字过滤（名称模糊匹配）与状态过滤
		if keyword != "" && !strings.Contains(strings.ToLower(view.Name), strings.ToLower(keyword)) {
			continue
		}
		if status != "" && status != "all" && view.Status != status {
			continue
		}
		result = append(result, view)
	}

	// 按创建时间倒序，新建的 Pod 排在最前
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt > result[j].CreatedAt })
	return result, nil
}

// Stats 汇总列表页顶部的四个统计数字。
func (s *PodService) Stats(ctx context.Context, userID int64) (map[string]int, error) {
	views, err := s.List(ctx, "", "", userID)
	if err != nil {
		return nil, err
	}
	stats := map[string]int{"running": 0, "stopped": 0, "failed": 0, "mine": 0}
	for _, v := range views {
		switch v.Status {
		case model.StatusRunning:
			stats["running"]++
		case model.StatusStopped:
			stats["stopped"]++
		case model.StatusFailed:
			stats["failed"]++
		}
		if v.MyRole == "负责人" {
			stats["mine"]++
		}
	}
	return stats, nil
}

// Get 返回详情页数据；Deployment 已被删除时仍返回元数据并标记 NotFound。
func (s *PodService) Get(ctx context.Context, name string, userID int64) (*model.PodDetail, error) {
	meta, _ := s.store.GetPodMeta(s.namespace, name)
	users, _ := s.store.ListUsers()

	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}

	// Deployment 不存在：以元数据兜底，状态置为 NotFound
	if apierrors.IsNotFound(err) || dep == nil {
		if meta == nil {
			return nil, store.ErrNotFound
		}
		return &model.PodDetail{
			PodView: model.PodView{
				Name:       meta.Name,
				Status:     model.StatusNotFound,
				StatusText: model.StatusText(model.StatusNotFound),
				Spec:       specOf(meta, corev1.Container{}),
				Image:      meta.Image,
				Owner:      ownerName(meta, users),
				MyRole:     myRole(meta, userID),
				Replicas:   meta.Replicas,
				CreatedAt:  meta.CreatedAt,
			},
			Namespace: s.namespace,
			CPU:       meta.CPU,
			Memory:    meta.Memory,
			Disk:      meta.Disk,
			Env:       meta.Env,
			Command:   meta.Command,
		}, nil
	}

	pods, _ := s.podsOf(ctx, name)
	status := calcStatus(dep, pods)
	container := containerOf(dep)

	cpuFallback, memFallback := "", ""
	if q, ok := container.Resources.Limits[corev1.ResourceCPU]; ok {
		cpuFallback = q.String()
	}
	if q, ok := container.Resources.Limits[corev1.ResourceMemory]; ok {
		memFallback = q.String()
	}

	detail := &model.PodDetail{
		PodView: model.PodView{
			Name:          dep.Name,
			Status:        status,
			StatusText:    model.StatusText(status),
			Spec:          specOf(meta, container),
			Image:         imageOf(meta, container),
			Owner:         ownerName(meta, users),
			MyRole:        myRole(meta, userID),
			Replicas:      replicasOf(dep),
			ReadyReplicas: dep.Status.ReadyReplicas,
			CreatedAt:     createdAtOf(meta, dep),
		},
		Namespace: s.namespace,
		CPU:       metaValue(meta, func(m *model.PodMeta) string { return m.CPU }, cpuFallback),
		Memory:    metaValue(meta, func(m *model.PodMeta) string { return m.Memory }, memFallback),
		Disk:      metaValue(meta, func(m *model.PodMeta) string { return m.Disk }, ""),
		Env:       []model.EnvVar{},
		Command:   []string{},
		PodName:   firstPodName(pods),
	}
	if meta != nil {
		detail.Env = meta.Env
		detail.Command = meta.Command
	}
	return detail, nil
}

// Create 创建业务的 Pod：先建 PVC，再建 Deployment，失败时回滚 PVC。
func (s *PodService) Create(ctx context.Context, req model.CreatePodRequest, ownerID int64) error {
	if err := validateCreate(&req); err != nil {
		return err
	}
	if req.Replicas < 0 {
		req.Replicas = 1
	}
	if req.Replicas == 0 {
		req.Replicas = 1 // 创建时至少 1 副本，停止请调用停止接口
	}
	if req.Disk == "" {
		req.Disk = "5Gi"
	}

	// 容错：后端可能先于集群就绪启动，这里再做一次命名空间检查
	s.client.ensureNamespace(ctx)

	// 名称占位检查，避免覆盖已有资源
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, req.Name, metav1.GetOptions{}); err == nil {
		return fmt.Errorf("Pod 名称 %s 已存在", req.Name)
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	if err := s.createPVC(ctx, req.Name, req.Disk); err != nil {
		return err
	}

	if err := s.createDeployment(ctx, req, ownerID); err != nil {
		// 创建失败时回收 PVC，避免留下孤立存储
		_ = s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).
			Delete(context.Background(), pvcName(req.Name), metav1.DeleteOptions{})
		return err
	}

	return s.store.CreatePodMeta(&model.PodMeta{
		Namespace: s.namespace,
		Name:      req.Name,
		OwnerID:   ownerID,
		Image:     req.Image,
		CPU:       req.CPU,
		Memory:    req.Memory,
		Disk:      req.Disk,
		Replicas:  req.Replicas,
		Env:       req.Env,
		Command:   req.Command,
	})
}

// createPVC 为 Pod 创建数据盘。
func (s *PodService) createPVC(ctx context.Context, name, disk string) error {
	diskQ, err := resource.ParseQuantity(disk)
	if err != nil {
		return fmt.Errorf("存储规格格式不合法: %s", disk)
	}
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName(name),
			Namespace: s.namespace,
			Labels:    map[string]string{appLabel: name, ManagedLabel: "true"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: diskQ},
			},
		},
	}
	// 指定 StorageClass（为空则交给集群默认 StorageClass）
	if s.client.StorageClass != "" {
		sc := s.client.StorageClass
		pvc.Spec.StorageClassName = &sc
	}

	if _, err := s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("创建存储失败: %w", err)
	}
	return nil
}

// createDeployment 创建底层 Deployment。
func (s *PodService) createDeployment(ctx context.Context, req model.CreatePodRequest, ownerID int64) error {
	cpuQ, err := resource.ParseQuantity(req.CPU)
	if err != nil {
		return fmt.Errorf("CPU 规格格式不合法: %s", req.CPU)
	}
	memQ, err := resource.ParseQuantity(req.Memory)
	if err != nil {
		return fmt.Errorf("内存规格格式不合法: %s", req.Memory)
	}

	labels := map[string]string{
		appLabel:     req.Name,
		ManagedLabel: "true",
	}
	// 启动命令为空时使用 sleep infinity，保证容器常驻可接入终端
	command := req.Command
	if len(command) == 0 {
		command = []string{"sleep", "infinity"}
	}
	env := make([]corev1.EnvVar, 0, len(req.Env))
	for _, e := range req.Env {
		if strings.TrimSpace(e.Key) == "" {
			continue
		}
		env = append(env, corev1.EnvVar{Name: e.Key, Value: e.Value})
	}

	replicas := req.Replicas
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: s.namespace,
			Labels:    labels,
			Annotations: map[string]string{
				"ssedeploy.io/owner-id": fmt.Sprintf("%d", ownerID),
				"ssedeploy.io/created-by": "ssedeploy",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{appLabel: req.Name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    mainContainer,
							Image:   req.Image,
							Command: command,
							Env:     env,
							// requests 与 limits 保持一致，便于 metrics 计算百分比
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    cpuQ,
									corev1.ResourceMemory: memQ,
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    cpuQ,
									corev1.ResourceMemory: memQ,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data", MountPath: "/data"},
							},
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName(req.Name),
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("创建 Pod 失败: %w", err)
	}
	return nil
}

// Update 修改镜像、规格、环境变量与启动命令；修改镜像/命令会自动触发滚动更新。
func (s *PodService) Update(ctx context.Context, name string, req model.UpdatePodRequest) error {
	cpuQ, err := resource.ParseQuantity(req.CPU)
	if err != nil {
		return fmt.Errorf("CPU 规格格式不合法: %s", req.CPU)
	}
	memQ, err := resource.ParseQuantity(req.Memory)
	if err != nil {
		return fmt.Errorf("内存规格格式不合法: %s", req.Memory)
	}

	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return store.ErrNotFound
		}
		return err
	}

	command := req.Command
	if len(command) == 0 {
		command = []string{"sleep", "infinity"}
	}
	env := make([]corev1.EnvVar, 0, len(req.Env))
	for _, e := range req.Env {
		if strings.TrimSpace(e.Key) == "" {
			continue
		}
		env = append(env, corev1.EnvVar{Name: e.Key, Value: e.Value})
	}

	// 直接修改主容器配置（找不到主容器时退回第一个容器）
	index := 0
	for i := range dep.Spec.Template.Spec.Containers {
		if dep.Spec.Template.Spec.Containers[i].Name == mainContainer {
			index = i
			break
		}
	}
	if len(dep.Spec.Template.Spec.Containers) > 0 {
		c := &dep.Spec.Template.Spec.Containers[index]
		c.Image = req.Image
		c.Command = command
		c.Env = env
		c.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: cpuQ, corev1.ResourceMemory: memQ},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: cpuQ, corev1.ResourceMemory: memQ},
		}
	}

	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("更新 Pod 失败: %w", err)
	}

	// 同步数据库中的元数据（存储容量与副本数不在此处修改）
	meta, err := s.store.GetPodMeta(s.namespace, name)
	if err == nil {
		meta.Image = req.Image
		meta.CPU = req.CPU
		meta.Memory = req.Memory
		meta.Env = req.Env
		meta.Command = req.Command
		if err := s.store.UpdatePodMeta(meta); err != nil {
			return err
		}
	}
	return nil
}

// Delete 删除 Pod：Deployment 与 PVC 一起回收。
func (s *PodService) Delete(ctx context.Context, name string) error {
	policy := metav1.DeletePropagationBackground
	err := s.client.Kube.AppsV1().Deployments(s.namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &policy,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("删除 Pod 失败: %w", err)
	}

	// PVC 删除失败不阻塞主流程（可能被其他工作负载占用）
	_ = s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).
		Delete(ctx, pvcName(name), metav1.DeleteOptions{})

	return s.store.DeletePodMeta(s.namespace, name)
}

// Scale 调整副本数：1 = 启动，0 = 停止。
func (s *PodService) Scale(ctx context.Context, name string, replicas int32) error {
	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return store.ErrNotFound
		}
		return err
	}
	dep.Spec.Replicas = &replicas
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("调整副本数失败: %w", err)
	}
	// 同步元数据中的副本数
	_ = s.store.UpdateReplicas(s.namespace, name, replicas)
	return nil
}

// Restart 通过修改模板注解触发滚动重启。
func (s *PodService) Restart(ctx context.Context, name string) error {
	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return store.ErrNotFound
		}
		return err
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = map[string]string{}
	}
	dep.Spec.Template.Annotations[RestartedAtAnnotation] = time.Now().Format(time.RFC3339)
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("重启 Pod 失败: %w", err)
	}
	return nil
}

// podsOf 返回某个 Pod（Deployment）下的全部真实 Pod。
func (s *PodService) podsOf(ctx context.Context, name string) ([]corev1.Pod, error) {
	list, err := s.client.Kube.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: appLabel + "=" + name + "," + ManagedLabel + "=true",
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// runningPod 选取一个可用 Pod（优先 Ready+Running），用于日志与终端。
func (s *PodService) runningPod(ctx context.Context, name string) (*corev1.Pod, error) {
	pods, err := s.podsOf(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("当前 Pod 已停止，没有可用的实例")
	}
	// 优先返回 Ready 的实例
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning && podReady(&pods[i]) {
			return &pods[i], nil
		}
	}
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			return &pods[i], nil
		}
	}
	return nil, fmt.Errorf("Pod 实例尚未就绪，请稍后再试")
}

// podReady 判断 Pod 是否所有容器都 Ready。
func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// calcStatus 实现业务状态映射：NotFound / 已停止 / 失败 / 运行中。
func calcStatus(dep *appsv1.Deployment, pods []corev1.Pod) string {
	if dep == nil {
		return model.StatusNotFound
	}
	if dep.Spec.Replicas != nil && *dep.Spec.Replicas == 0 {
		return model.StatusStopped
	}
	for i := range pods {
		if podFailed(&pods[i]) {
			return model.StatusFailed
		}
	}
	return model.StatusRunning
}

// podFailed 检测常见的不可用原因（镜像拉取、CrashLoop、配置错误等）。
func podFailed(p *corev1.Pod) bool {
	if p.Status.Phase == corev1.PodFailed {
		return true
	}
	check := func(statuses []corev1.ContainerStatus) bool {
		for _, st := range statuses {
			if st.State.Waiting == nil {
				continue
			}
			switch st.State.Waiting.Reason {
			case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError", "InvalidImageName":
				return true
			}
		}
		return false
	}
	return check(p.Status.InitContainerStatuses) || check(p.Status.ContainerStatuses)
}

// replicasOf 读取期望副本数（未设置时视为 1）。
func replicasOf(dep *appsv1.Deployment) int32 {
	if dep.Spec.Replicas == nil {
		return 1
	}
	return *dep.Spec.Replicas
}

// specOf 生成 "2C · 4Gi · 5Gi" 形式的规格文案。
func specOf(meta *model.PodMeta, c corev1.Container) string {
	cpu, mem, disk := "", "", ""
	if meta != nil {
		cpu, mem, disk = meta.CPU, meta.Memory, meta.Disk
	}
	if cpu == "" {
		if q, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
			cpu = q.String()
		}
	}
	if mem == "" {
		if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			mem = q.String()
		}
	}
	parts := make([]string, 0, 3)
	if cpu != "" {
		parts = append(parts, cpu+"C")
	}
	if mem != "" {
		parts = append(parts, mem)
	}
	if disk != "" {
		parts = append(parts, disk)
	}
	return strings.Join(parts, " · ")
}

// imageOf 取镜像地址，优先使用平台元数据。
func imageOf(meta *model.PodMeta, c corev1.Container) string {
	if meta != nil && meta.Image != "" {
		return meta.Image
	}
	return c.Image
}

// ownerName 把 owner_id 映射为展示名。
func ownerName(meta *model.PodMeta, users map[int64]*model.User) string {
	if meta == nil {
		return "-"
	}
	if u, ok := users[meta.OwnerID]; ok && u != nil {
		return u.Display()
	}
	return "-"
}

// myRole 计算当前用户对该 Pod 的角色。
func myRole(meta *model.PodMeta, userID int64) string {
	if meta != nil && meta.OwnerID == userID {
		return "负责人"
	}
	return "协作者"
}

// createdAtOf 优先使用元数据中的创建时间，保证删除重建后顺序正确。
func createdAtOf(meta *model.PodMeta, dep *appsv1.Deployment) int64 {
	if meta != nil && meta.CreatedAt > 0 {
		return meta.CreatedAt
	}
	return dep.CreationTimestamp.Unix()
}

// metaValue 元数据优先，缺失时回退到 Deployment 中的实际值。
func metaValue(meta *model.PodMeta, fromMeta func(*model.PodMeta) string, fallback string) string {
	if meta != nil {
		if v := fromMeta(meta); v != "" {
			return v
		}
	}
	return fallback
}

// firstPodName 返回第一个真实 Pod 名，供日志/终端定位。
func firstPodName(pods []corev1.Pod) string {
	for i := range pods {
		if pods[i].Status.Phase == corev1.PodRunning {
			return pods[i].Name
		}
	}
	if len(pods) > 0 {
		return pods[0].Name
	}
	return ""
}

// validateCreate 校验创建参数。
func validateCreate(req *model.CreatePodRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Image = strings.TrimSpace(req.Image)
	if req.Name == "" {
		return fmt.Errorf("Pod 名称不能为空")
	}
	if len(req.Name) > 40 || !nameRe.MatchString(req.Name) {
		return fmt.Errorf("Pod 名称需为 1-40 位小写字母、数字或短横线，且以字母或数字开头结尾")
	}
	if req.Image == "" {
		return fmt.Errorf("容器镜像不能为空")
	}
	if _, err := resource.ParseQuantity(req.CPU); err != nil || req.CPU == "" {
		return fmt.Errorf("CPU 规格格式不合法")
	}
	if _, err := resource.ParseQuantity(req.Memory); err != nil || req.Memory == "" {
		return fmt.Errorf("内存规格格式不合法")
	}
	if req.Disk != "" {
		if _, err := resource.ParseQuantity(req.Disk); err != nil {
			return fmt.Errorf("存储规格格式不合法")
		}
	}
	return nil
}
