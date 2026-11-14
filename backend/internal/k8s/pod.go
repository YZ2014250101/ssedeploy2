package k8s

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

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
	// servicePort 是自动创建的 NodePort Service 暴露的容器端口。
	// 平台约定用户 Web 服务监听该端口，外部通过自动分配的 NodePort 访问。
	servicePort int32 = 5173
)

// nameRe 校验 Pod（Deployment）名称，遵循 K8s DNS-1123 规则
var nameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// SSHPortNotifier 由 SSH 网关实现：
// 凭证签发时开启对应端口监听，Pod 删除时释放端口。
type SSHPortNotifier interface {
	// EnablePort 尝试监听端口，返回 false 表示该端口在本机不可用
	EnablePort(port int32) bool
	DisablePort(port int32)
}

// PodService 是页面语义上的 "Pod"：一个 Pod 在底层对应一个 Deployment + 一个 PVC + 一个 NodePort Service。
type PodService struct {
	client      *Client
	store       *store.Store
	namespace   string
	nodePortMin int32
	nodePortMax int32
	// SSH 网关信息：当前为云平台提供的占位凭证，可通过环境变量随时替换
	sshHost     string
	sshPortBase int32
	sshPortMax  int32
	sshUser     string
	sshNotifier SSHPortNotifier
}

// SetSSHPortNotifier 注入 SSH 网关：凭证签发后自动在该端口启动 SSH 服务。
func (s *PodService) SetSSHPortNotifier(n SSHPortNotifier) { s.sshNotifier = n }

// NewPodService 创建 Pod 业务服务。
// nodePortMin/nodePortMax 是自动创建的 NodePort Service 的端口分配范围；
// sshHost/sshPortBase/sshPortMax/sshUser 用于为每个 Pod 分配独立的 SSH 端口。
func NewPodService(client *Client, st *store.Store, nodePortMin, nodePortMax int32, sshHost string, sshPortBase, sshPortMax int32, sshUser string) *PodService {
	if nodePortMin <= 0 {
		nodePortMin = 30000
	}
	if nodePortMax <= 0 {
		nodePortMax = 30300
	}
	if nodePortMax < nodePortMin {
		nodePortMax = nodePortMin
	}
	if sshPortBase <= 0 {
		sshPortBase = 22057
	}
	if sshPortMax < sshPortBase {
		sshPortMax = sshPortBase + 999
	}
	return &PodService{
		client:      client,
		store:       st,
		namespace:   client.Namespace,
		nodePortMin: nodePortMin,
		nodePortMax: nodePortMax,
		sshHost:     sshHost,
		sshPortBase: sshPortBase,
		sshPortMax:  sshPortMax,
		sshUser:     sshUser,
	}
}

// Namespace 返回业务命名空间。
func (s *PodService) Namespace() string { return s.namespace }

// pvcName 返回 Pod 数据盘 PVC 名称。
func pvcName(name string) string { return name + "-data" }

// serviceName 返回 Pod 自动创建的外部访问 Service 名称。
func serviceName(name string) string { return name + "-web" }

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
// 管理员可看到全部 Pod；普通用户只能看到自己创建的 Pod。
func (s *PodService) List(ctx context.Context, keyword, status string, userID int64, isAdmin bool) ([]model.PodView, error) {
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
		// 归属过滤：普通用户只能看到自己创建的 Pod
		ownerID := ownerIDOf(meta, dep)
		if !isAdmin && ownerID != userID {
			continue
		}
		code := calcStatus(dep, podsByApp[dep.Name])

		view := model.PodView{
			Name:          dep.Name,
			Status:        code,
			StatusText:    model.StatusText(code),
			Spec:          specOf(meta, containerOf(dep)),
			Image:         imageOf(meta, containerOf(dep)),
			Owner:         ownerName(meta, users),
			MyRole:        myRole(ownerID, userID, isAdmin),
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
func (s *PodService) Stats(ctx context.Context, userID int64, isAdmin bool) (map[string]int, error) {
	views, err := s.List(ctx, "", "", userID, isAdmin)
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
// 普通用户访问他人创建的 Pod 时返回 ErrPodForbidden。
func (s *PodService) Get(ctx context.Context, name string, userID int64, isAdmin bool) (*model.PodDetail, error) {
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
		if !isAdmin && meta.OwnerID != userID {
			return nil, ErrPodForbidden
		}
		return &model.PodDetail{
			PodView: model.PodView{
				Name:       meta.Name,
				Status:     model.StatusNotFound,
				StatusText: model.StatusText(model.StatusNotFound),
				Spec:       specOf(meta, corev1.Container{}),
				Image:      meta.Image,
				Owner:      ownerName(meta, users),
				MyRole:     myRole(meta.OwnerID, userID, isAdmin),
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

	// 归属校验：普通用户只能查看自己创建的 Pod
	ownerID := ownerIDOf(meta, dep)
	if !isAdmin && ownerID != userID {
		return nil, ErrPodForbidden
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
			MyRole:        myRole(ownerID, userID, isAdmin),
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
	// 外部访问 Service：优先使用自动创建的 <name>-web，兼容手工创建的 Service
	if svc, err := s.findService(ctx, name); err == nil && svc != nil {
		detail.ServiceName = svc.Name
		for _, port := range svc.Spec.Ports {
			if port.NodePort != 0 {
				detail.NodePort = port.NodePort
				detail.ServicePort = port.Port
				break
			}
		}
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

	// 全局共享目录 PVC：所有用户 Pod 共用同一个 /shared 数据卷
	if err := s.ensureSharedPVC(ctx); err != nil {
		return fmt.Errorf("初始化共享存储失败: %w", err)
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

	// 自动创建 NodePort Service，让 Pod 内的 Web 服务可被外部访问
	if err := s.createService(ctx, req.Name, ownerID); err != nil {
		// Service 创建失败时回滚 Deployment 与 PVC，保证 Pod 创建是原子的
		policy := metav1.DeletePropagationBackground
		_ = s.client.Kube.AppsV1().Deployments(s.namespace).Delete(context.Background(), req.Name, metav1.DeleteOptions{
			PropagationPolicy: &policy,
		})
		_ = s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).
			Delete(context.Background(), pvcName(req.Name), metav1.DeleteOptions{})
		return err
	}

	if err := s.store.CreatePodMeta(&model.PodMeta{
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
	}); err != nil {
		return err
	}

	// 为新建 Pod 签发 SSH 凭证（每个 Pod 一份随机密码）。
	// 签发失败不阻塞 Pod 创建，首次打开「凭证」页时会自动补发。
	if _, err := s.EnsureSSHCredential(ctx, req.Name); err != nil {
		log.Printf("为 Pod %s 生成 SSH 凭证失败: %v", req.Name, err)
	}
	return nil
}

// EnsureSSHCredential 返回 Pod 的 SSH 凭证；数据库中不存在时按配置自动签发并落库。
// 每个 Pod 都会分配一个不与其他 Pod 冲突的专属端口，凭证创建后端口保持不变。
func (s *PodService) EnsureSSHCredential(ctx context.Context, name string) (*model.SSHCredential, error) {
	cred, err := s.store.GetSSHCredential(s.namespace, name)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}

	// 尚未签发凭证时，先确认 Pod（Deployment）真实存在，避免为任意名称生成凭据
	if cred == nil {
		if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, store.ErrNotFound
			}
			return nil, err
		}
	}

	// 分配或复用该 Pod 的专属 SSH 端口
	port, err := s.allocateSSHPort(name, cred)
	if err != nil {
		return nil, err
	}

	if cred == nil {
		password, err := generatePassword(24)
		if err != nil {
			return nil, fmt.Errorf("生成 SSH 密码失败: %w", err)
		}
		cred = &model.SSHCredential{
			Namespace: s.namespace,
			PodName:   name,
			Host:      s.sshHost,
			Port:      port,
			Username:  s.sshUser,
			Password:  password,
		}
		if err := s.store.SaveSSHCredential(cred); err != nil {
			return nil, err
		}
		// 回读一次，拿到数据库生成的创建时间
		saved, err := s.store.GetSSHCredential(s.namespace, name)
		if err != nil {
			return nil, err
		}
		return s.withSSHEndpoint(saved), nil
	}

	// 历史凭证可能与其他 Pod 共用同一端口（早期固定端口版本），检测到冲突后重新分配
	if cred.Port != port {
		cred.Port = port
		if err := s.store.SaveSSHCredential(cred); err != nil {
			return nil, err
		}
	}
	return s.withSSHEndpoint(cred), nil
}

// allocateSSHPort 为 Pod 选择 SSH 端口：优先复用其已有端口，
// 否则从起始端口开始取第一个未被其他 Pod 占用、且本机可监听的端口。
func (s *PodService) allocateSSHPort(podName string, cred *model.SSHCredential) (int32, error) {
	owners, err := s.store.UsedSSHPorts(s.namespace)
	if err != nil {
		return 0, err
	}

	// 已持有一个只属于自己的端口时保持不变，避免用户本地 SSH 配置失效
	if cred != nil && cred.Port > 0 {
		if owner, used := owners[cred.Port]; !used || owner == podName {
			if s.canListenSSH(cred.Port) {
				return cred.Port, nil
			}
		}
	}

	for port := s.sshPortBase; port <= s.sshPortMax; port++ {
		if owner, used := owners[port]; used && owner != podName {
			continue
		}
		// 端口在本机被系统保留或被其他进程占用时跳过，避免凭证指向无法监听的端口
		if s.canListenSSH(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("SSH 端口已耗尽（%d-%d），请调整 SSD_SSH_PORT / SSD_SSH_PORT_MAX", s.sshPortBase, s.sshPortMax)
}

// canListenSSH 让 SSH 网关确认端口可用；未注入网关时视为可用。
func (s *PodService) canListenSSH(port int32) bool {
	if s.sshNotifier == nil {
		return true
	}
	return s.sshNotifier.EnablePort(port)
}

// withSSHEndpoint 用当前配置覆盖凭证中的网关地址与用户名，
// 更换 SSH 网关只需改配置即可对已有 Pod 生效；端口是每 Pod 专属的，保持库中记录不变。
func (s *PodService) withSSHEndpoint(c *model.SSHCredential) *model.SSHCredential {
	if s.sshHost != "" {
		c.Host = s.sshHost
	}
	if s.sshUser != "" {
		c.Username = s.sshUser
	}
	return c
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
				"ssedeploy.io/owner-id":   fmt.Sprintf("%d", ownerID),
				"ssedeploy.io/created-by": "ssedeploy",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			// 数据盘为 RWO 的 PVC，滚动更新会出现“新 Pod 等旧 Pod 释放存储”的死锁，
			// 因此统一使用 Recreate：先删旧 Pod，再创建新 Pod。
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
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
							// 同一个 PVC 同时挂到 /data 与 /home/workspace：
							// 编辑器文件树、Web 终端都基于 /home/workspace，保证看到同一份数据
							VolumeMounts: []corev1.VolumeMount{
								{Name: dataVolumeName, MountPath: "/data"},
								{Name: dataVolumeName, MountPath: WorkspacePath},
								// 全局共享目录：所有 Pod 挂载同一个 PVC，实现跨 Pod 共享
								{Name: sharedVolumeName, MountPath: SharedPath},
							},
							WorkingDir:      WorkspacePath,
							ImagePullPolicy: corev1.PullIfNotPresent,
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: dataVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName(req.Name),
								},
							},
						},
						{
							// 全局共享目录，与后端 /shared 指向同一份数据
							Name: sharedVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: SharedPVCName,
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
// 普通用户只能修改自己创建的 Pod，管理员可修改全部。
func (s *PodService) Update(ctx context.Context, name string, req model.UpdatePodRequest, userID int64, isAdmin bool) error {
	if err := s.EnsureAccess(ctx, name, userID, isAdmin); err != nil {
		return err
	}

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
// 普通用户只能删除自己创建的 Pod，管理员可删除全部。
func (s *PodService) Delete(ctx context.Context, name string, userID int64, isAdmin bool) error {
	if err := s.EnsureAccess(ctx, name, userID, isAdmin); err != nil {
		return err
	}

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

	// 一并回收自动创建的外部访问 Service，避免端口长期被占用
	_ = s.client.Kube.CoreV1().Services(s.namespace).
		Delete(ctx, serviceName(name), metav1.DeleteOptions{})

	// 回收 SSH 凭证与端口监听，避免 Pod 删除后仍可凭旧密码登录
	if cred, err := s.store.GetSSHCredential(s.namespace, name); err == nil && s.sshNotifier != nil {
		s.sshNotifier.DisablePort(cred.Port)
	}
	_ = s.store.DeleteSSHCredential(s.namespace, name)

	return s.store.DeletePodMeta(s.namespace, name)
}

// createService 为 Pod 自动创建 NodePort Service，自动分配一个未被占用的端口。
// Service 选择器为 app=<Pod 名称>，暴露约定的容器端口 servicePort。
func (s *PodService) createService(ctx context.Context, name string, ownerID int64) error {
	svcName := serviceName(name)
	if _, err := s.client.Kube.CoreV1().Services(s.namespace).Get(ctx, svcName, metav1.GetOptions{}); err == nil {
		return nil
	} else if !apierrors.IsNotFound(err) {
		return err
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		port, err := s.allocateNodePort(ctx)
		if err != nil {
			return err
		}
		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: s.namespace,
				Labels: map[string]string{
					appLabel:     name,
					ManagedLabel: "true",
				},
				Annotations: map[string]string{
					"ssedeploy.io/owner": strconv.FormatInt(ownerID, 10),
				},
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: map[string]string{appLabel: name},
				Ports: []corev1.ServicePort{{
					Name:       "web",
					Protocol:   corev1.ProtocolTCP,
					Port:       servicePort,
					TargetPort: intstr.FromInt32(servicePort),
					NodePort:   port,
				}},
			},
		}
		if _, err := s.client.Kube.CoreV1().Services(s.namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
			if apierrors.IsAlreadyExists(err) {
				return nil
			}
			lastErr = err
			// 并发创建时端口可能被抢占，重新分配后重试
			if apierrors.IsConflict(err) || strings.Contains(err.Error(), "already allocated") {
				continue
			}
			break
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("没有可用的 NodePort")
	}
	return fmt.Errorf("创建外部访问 Service 失败: %w", lastErr)
}

// allocateNodePort 在配置范围内挑选一个未被任何 Service 占用的 NodePort。
func (s *PodService) allocateNodePort(ctx context.Context) (int32, error) {
	list, err := s.client.Kube.CoreV1().Services(s.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("读取 Service 列表失败: %w", err)
	}
	used := make(map[int32]bool, len(list.Items)*2)
	for _, svc := range list.Items {
		for _, port := range svc.Spec.Ports {
			if port.NodePort > 0 {
				used[port.NodePort] = true
			}
		}
	}
	for port := s.nodePortMin; port <= s.nodePortMax; port++ {
		if !used[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("NodePort 已耗尽（%d-%d），请删除闲置 Pod 后重试", s.nodePortMin, s.nodePortMax)
}

// findService 查找 Pod 对应的外部访问 Service：
// 优先返回平台自动创建的 <name>-web，兼容用户手工创建的同选择器 Service。
func (s *PodService) findService(ctx context.Context, name string) (*corev1.Service, error) {
	svc, err := s.client.Kube.CoreV1().Services(s.namespace).Get(ctx, serviceName(name), metav1.GetOptions{})
	if err == nil {
		return svc, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	list, err := s.client.Kube.CoreV1().Services(s.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for i := range list.Items {
		if list.Items[i].Spec.Selector[appLabel] == name {
			return &list.Items[i], nil
		}
	}
	return nil, nil
}

// Scale 调整副本数：1 = 启动，0 = 停止。
// 普通用户只能操作自己创建的 Pod，管理员可操作全部。
func (s *PodService) Scale(ctx context.Context, name string, replicas int32, userID int64, isAdmin bool) error {
	if err := s.EnsureAccess(ctx, name, userID, isAdmin); err != nil {
		return err
	}

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
// 普通用户只能操作自己创建的 Pod，管理员可操作全部。
func (s *PodService) Restart(ctx context.Context, name string, userID int64, isAdmin bool) error {
	if err := s.EnsureAccess(ctx, name, userID, isAdmin); err != nil {
		return err
	}

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

// ownerIDOf 取 Pod 负责人：优先数据库元数据，缺失时回退 Deployment 上的注解。
func ownerIDOf(meta *model.PodMeta, dep *appsv1.Deployment) int64 {
	if meta != nil {
		return meta.OwnerID
	}
	if dep == nil {
		return 0
	}
	if raw := dep.Annotations["ssedeploy.io/owner-id"]; raw != "" {
		id, _ := strconv.ParseInt(raw, 10, 64)
		return id
	}
	return 0
}

// myRole 计算当前用户对该 Pod 的角色：本人 -> 负责人，管理员 -> 管理员，其余 -> 协作者。
func myRole(ownerID, userID int64, isAdmin bool) string {
	if ownerID != 0 && ownerID == userID {
		return "负责人"
	}
	if isAdmin {
		return "管理员"
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
