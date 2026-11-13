package k8s

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

// MiddlewareLabel 标记资源属于某个中间件实例。
// 注意：这里刻意不使用 ManagedLabel，避免中间件被当成用户 Pod 出现在 Pod 列表里。
const MiddlewareLabel = "ssedeploy.io/middleware"

// ErrInvalidMiddleware 表示中间件创建参数不合法，接口层映射为 400。
var ErrInvalidMiddleware = errors.New("中间件参数不合法")

// versionRe 限制镜像版本号字符，防止把任意字符串拼进镜像地址。
var versionRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// dbNameRe 限制 MySQL 初始库名。
var dbNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// passwordAlphabet 生成密码用的字符集：剔除引号、反斜杠、$、反引号等
// 容易被 shell / YAML 转义踩坑的字符，同时包含大小写、数字与符号保证强度。
const passwordAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789-#%+="

// middlewareSpec 描述一种中间件的部署形态。
// 新增中间件类型（Redis、PostgreSQL 等）只需在 middlewareRegistry 中追加一项。
type middlewareSpec struct {
	Type            string
	ImageTemplate   string // %s 由版本号替换
	DefaultVersion  string
	Port            int32
	PortName        string
	DataPath        string // 数据目录挂载点
	Username        string // 固定用户名（MySQL 为 root，MinIO 为控制台账号）
	DefaultStorage  string
	DefaultDatabase string
	Command         []string                                  // 覆盖镜像 ENTRYPOINT（一般不使用）
	Args            []string                                  // 传给镜像 ENTRYPOINT 的参数（MinIO 需要）
	Env             func(m *model.Middleware) []corev1.EnvVar // 环境变量（密码通过 Secret 注入）
	Readiness       *corev1.Probe                             // 就绪探针
}

// middlewareRegistry 中间件类型注册表：新增类型只需在此扩展，其余流程通用。
var middlewareRegistry = map[string]middlewareSpec{
	model.MiddlewareMySQL: {
		Type:            model.MiddlewareMySQL,
		ImageTemplate:   "mysql:%s",
		DefaultVersion:  "8.0",
		Port:            3306,
		PortName:        "mysql",
		DataPath:        "/var/lib/mysql",
		Username:        "root",
		DefaultStorage:  "5Gi",
		DefaultDatabase: "app",
		Env: func(m *model.Middleware) []corev1.EnvVar {
			env := []corev1.EnvVar{
				// 密码不写进 Deployment 明文，而是引用 Secret
				{Name: "MYSQL_ROOT_PASSWORD", ValueFrom: secretKeyRef(m.Name)},
			}
			if m.Database != "" {
				env = append(env, corev1.EnvVar{Name: "MYSQL_DATABASE", Value: m.Database})
			}
			return env
		},
		// mysqladmin ping 通过环境变量取密码，避免特殊字符转义问题
		Readiness: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"/bin/sh", "-c", `mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" --silent`},
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    6,
		},
	},
	model.MiddlewareMinIO: {
		Type:            model.MiddlewareMinIO,
		ImageTemplate:   "minio/minio:%s",
		DefaultVersion:  "RELEASE.2024-06-13T22-53-53Z",
		Port:            9000,
		PortName:        "s3",
		DataPath:        "/data",
		Username:        "minioadmin",
		DefaultStorage:  "10Gi",
		DefaultDatabase: "",
		// 不能写 Command：覆盖 ENTRYPOINT 后会直接 exec "server" 导致
		// executable file not found。改用 Args，由镜像的 docker-entrypoint.sh
		// 补全为 minio server /data。
		Args: []string{"server", "/data"},
		Env: func(m *model.Middleware) []corev1.EnvVar {
			return []corev1.EnvVar{
				{Name: "MINIO_ROOT_USER", Value: m.Username},
				{Name: "MINIO_ROOT_PASSWORD", ValueFrom: secretKeyRef(m.Name)},
			}
		},
		Readiness: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{Path: "/minio/health/live", Port: intstr.FromInt32(9000)},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
			TimeoutSeconds:      3,
			FailureThreshold:    6,
		},
	},
}

// MiddlewareService 负责中间件实例的生命周期管理。
type MiddlewareService struct {
	client    *Client
	store     *store.Store
	namespace string
}

// NewMiddlewareService 创建中间件业务服务。
func NewMiddlewareService(client *Client, st *store.Store) *MiddlewareService {
	return &MiddlewareService{client: client, store: st, namespace: client.Namespace}
}

// middlewareSecretName 返回存放密码的 Secret 名称。
func middlewareSecretName(name string) string { return name + "-secret" }

// secretKeyRef 构造指向中间件密码 Secret 的引用。
func secretKeyRef(name string) *corev1.EnvVarSource {
	return &corev1.EnvVarSource{
		SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: middlewareSecretName(name)},
			Key:                  "password",
		},
	}
}

// Create 一键创建中间件：PVC → Secret → Deployment → Service，最后落库连接信息。
// 任一步失败都会回滚已创建的资源，保证不留下半成品。
func (s *MiddlewareService) Create(ctx context.Context, podName string, ownerID int64, req model.CreateMiddlewareRequest) (*model.Middleware, error) {
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type == "" {
		req.Type = model.MiddlewareMySQL
	}
	spec, ok := middlewareRegistry[req.Type]
	if !ok {
		return nil, fmt.Errorf("%w: 暂不支持的类型 %s", ErrInvalidMiddleware, req.Type)
	}

	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 40 || !nameRe.MatchString(name) {
		return nil, fmt.Errorf("%w: 服务名需为 1-40 位小写字母、数字或短横线，且以字母或数字开头结尾", ErrInvalidMiddleware)
	}

	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = spec.DefaultVersion
	}
	if !versionRe.MatchString(version) {
		return nil, fmt.Errorf("%w: 版本号仅允许字母、数字、点、下划线和短横线", ErrInvalidMiddleware)
	}

	storage := strings.TrimSpace(req.Storage)
	if storage == "" {
		storage = spec.DefaultStorage
	}
	diskQ, err := resource.ParseQuantity(storage)
	if err != nil || diskQ.Sign() <= 0 {
		return nil, fmt.Errorf("%w: 存储规格不合法", ErrInvalidMiddleware)
	}

	database := strings.TrimSpace(req.Database)
	if database == "" {
		database = spec.DefaultDatabase
	}
	if database != "" && !dbNameRe.MatchString(database) {
		return nil, fmt.Errorf("%w: 库名仅允许字母、数字、下划线和短横线", ErrInvalidMiddleware)
	}

	// 名称唯一性：数据库记录 / Deployment / Service 任一已存在都拒绝
	if _, err := s.store.GetMiddleware(s.namespace, name); err == nil {
		return nil, store.ErrDuplicate
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		return nil, fmt.Errorf("%w: 名称 %s 已被其他工作负载占用", ErrInvalidMiddleware, name)
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}
	if _, err := s.client.Kube.CoreV1().Services(s.namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		return nil, fmt.Errorf("%w: 名称 %s 已被其他 Service 占用", ErrInvalidMiddleware, name)
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	password, err := generatePassword(24)
	if err != nil {
		return nil, fmt.Errorf("生成随机密码失败: %w", err)
	}

	inst := &model.Middleware{
		Namespace: s.namespace,
		PodName:   podName,
		OwnerID:   ownerID,
		Type:      spec.Type,
		Name:      name,
		Version:   version,
		Image:     fmt.Sprintf(spec.ImageTemplate, version),
		Storage:   diskQ.String(),
		Username:  spec.Username,
		Password:  password,
		Database:  database,
		Port:      spec.Port,
		// 集群内其他 Pod 通过该域名 + 端口访问，例如 mysql-1.ssedeploy.svc.cluster.local:3306
		Host: fmt.Sprintf("%s.%s.svc.cluster.local", name, s.namespace),
	}

	// 逐项创建，失败即回滚，避免残留孤立资源
	if err := s.createPVC(ctx, inst, diskQ); err != nil {
		return nil, err
	}
	if err := s.createSecret(ctx, inst); err != nil {
		s.rollback(ctx, inst, true)
		return nil, err
	}
	if err := s.createDeployment(ctx, inst, spec); err != nil {
		s.rollback(ctx, inst, true)
		return nil, err
	}
	if err := s.createService(ctx, inst, spec); err != nil {
		s.rollback(ctx, inst, true)
		return nil, err
	}

	if err := s.store.CreateMiddleware(inst); err != nil {
		s.rollback(ctx, inst, true)
		return nil, err
	}

	// 回读一次，拿到数据库生成的创建时间
	if saved, err := s.store.GetMiddleware(s.namespace, inst.Name); err == nil {
		inst = saved
	}
	inst.Host = s.hostOf(inst.Name)
	inst.Status = model.MiddlewareCreating
	return inst, nil
}

// hostOf 返回中间件在集群内的访问域名（Host 属于派生字段，不落库）。
func (s *MiddlewareService) hostOf(name string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", name, s.namespace)
}

// List 返回某个 Pod 名下的中间件，并实时探测 K8s 中的运行状态。
func (s *MiddlewareService) List(ctx context.Context, podName string) ([]*model.Middleware, error) {
	items, err := s.store.ListMiddlewares(s.namespace, podName)
	if err != nil {
		return nil, err
	}
	result := make([]*model.Middleware, 0, len(items))
	for _, m := range items {
		m.Host = s.hostOf(m.Name)
		m.Status = s.statusOf(ctx, m.Name)
		result = append(result, m)
	}
	return result, nil
}

// Delete 删除中间件：级联回收 Deployment / Service / Secret。
// keepPVC=true 时保留数据盘，便于重建同名实例后恢复数据；默认保留。
func (s *MiddlewareService) Delete(ctx context.Context, podName, name string, keepPVC bool) error {
	inst, err := s.store.GetMiddleware(s.namespace, name)
	if err != nil {
		return err
	}
	// 归属校验：只能删除本 Pod 名下的中间件
	if inst.PodName != podName {
		return store.ErrNotFound
	}

	policy := metav1.DeletePropagationBackground
	if err := s.client.Kube.AppsV1().Deployments(s.namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &policy,
	}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("删除中间件工作负载失败: %w", err)
	}
	_ = s.client.Kube.CoreV1().Services(s.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	_ = s.client.Kube.CoreV1().Secrets(s.namespace).Delete(ctx, middlewareSecretName(name), metav1.DeleteOptions{})
	if !keepPVC {
		_ = s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).Delete(ctx, pvcName(name), metav1.DeleteOptions{})
	}

	return s.store.DeleteMiddleware(s.namespace, name)
}

// statusOf 依据 Deployment 与 Pod 状态推导中间件展示状态。
func (s *MiddlewareService) statusOf(ctx context.Context, name string) string {
	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return model.MiddlewareNotFound
	}
	if err != nil {
		// 瞬时错误不放大为失败，按启动中处理
		return model.MiddlewareCreating
	}
	if dep.Status.ReadyReplicas > 0 {
		return model.MiddlewareRunning
	}
	for _, p := range s.podsOf(ctx, name) {
		item := p
		if podFailed(&item) {
			return model.MiddlewareFailed
		}
	}
	return model.MiddlewareCreating
}

// podsOf 返回某个中间件 Deployment 下的 Pod 列表。
func (s *MiddlewareService) podsOf(ctx context.Context, name string) []corev1.Pod {
	list, err := s.client.Kube.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: appLabel + "=" + name,
	})
	if err != nil {
		return nil
	}
	return list.Items
}

// createPVC 创建数据盘；StorageClass 与平台 Pod 保持一致。
func (s *MiddlewareService) createPVC(ctx context.Context, inst *model.Middleware, diskQ resource.Quantity) error {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName(inst.Name),
			Namespace: s.namespace,
			Labels:    s.labels(inst),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: diskQ},
			},
		},
	}
	if s.client.StorageClass != "" {
		sc := s.client.StorageClass
		pvc.Spec.StorageClassName = &sc
	}
	if _, err := s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).Create(ctx, pvc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("创建中间件存储失败: %w", err)
	}
	return nil
}

// createSecret 保存自动生成的密码，Deployment 通过 secretKeyRef 引用。
func (s *MiddlewareService) createSecret(ctx context.Context, inst *model.Middleware) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      middlewareSecretName(inst.Name),
			Namespace: s.namespace,
			Labels:    s.labels(inst),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{"password": []byte(inst.Password)},
	}
	if _, err := s.client.Kube.CoreV1().Secrets(s.namespace).Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("创建密码 Secret 失败: %w", err)
	}
	return nil
}

// createDeployment 创建中间件工作负载。
// 使用 Recreate 策略：数据盘为 RWO，滚动更新会出现新旧 Pod 争抢存储的死锁。
func (s *MiddlewareService) createDeployment(ctx context.Context, inst *model.Middleware, spec middlewareSpec) error {
	replicas := int32(1)
	cpuRequest := resource.MustParse("250m")
	memRequest := resource.MustParse("512Mi")
	cpuLimit := resource.MustParse("1")
	memLimit := resource.MustParse("1Gi")

	container := corev1.Container{
		Name:    inst.Type,
		Image:   inst.Image,
		Command: spec.Command,
		Args:    spec.Args,
		Env:     spec.Env(inst),
		Ports: []corev1.ContainerPort{
			{Name: spec.PortName, ContainerPort: spec.Port, Protocol: corev1.ProtocolTCP},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: spec.DataPath},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: cpuRequest, corev1.ResourceMemory: memRequest},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: cpuLimit, corev1.ResourceMemory: memLimit},
		},
		ImagePullPolicy: corev1.PullIfNotPresent,
	}
	if spec.Readiness != nil {
		container.ReadinessProbe = spec.Readiness
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      inst.Name,
			Namespace: s.namespace,
			Labels:    s.labels(inst),
			Annotations: map[string]string{
				"ssedeploy.io/owner-id":   fmt.Sprintf("%d", inst.OwnerID),
				"ssedeploy.io/created-by": "ssedeploy",
				"ssedeploy.io/pod":        inst.PodName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{appLabel: inst.Name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: s.labels(inst)},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{container},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvcName(inst.Name),
								},
							},
						},
					},
				},
			},
		},
	}
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("创建中间件工作负载失败: %w", err)
	}
	return nil
}

// createService 创建 ClusterIP Service，名称与用户传入的服务名一致，供集群内访问。
func (s *MiddlewareService) createService(ctx context.Context, inst *model.Middleware, spec middlewareSpec) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      inst.Name,
			Namespace: s.namespace,
			Labels:    s.labels(inst),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{appLabel: inst.Name},
			Ports: []corev1.ServicePort{
				{
					Name:       spec.PortName,
					Protocol:   corev1.ProtocolTCP,
					Port:       spec.Port,
					TargetPort: intstr.FromInt32(spec.Port),
				},
			},
		},
	}
	if _, err := s.client.Kube.CoreV1().Services(s.namespace).Create(ctx, svc, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("创建中间件 Service 失败: %w", err)
	}
	return nil
}

// labels 返回中间件资源统一的标签集合（app 用于关联 Service / Pod）。
func (s *MiddlewareService) labels(inst *model.Middleware) map[string]string {
	return map[string]string{
		appLabel:        inst.Name,
		MiddlewareLabel: inst.Type,
	}
}

// rollback 回滚已创建的中间件资源；withPVC=false 时保留数据盘。
func (s *MiddlewareService) rollback(ctx context.Context, inst *model.Middleware, withPVC bool) {
	policy := metav1.DeletePropagationBackground
	_ = s.client.Kube.AppsV1().Deployments(s.namespace).Delete(ctx, inst.Name, metav1.DeleteOptions{
		PropagationPolicy: &policy,
	})
	_ = s.client.Kube.CoreV1().Services(s.namespace).Delete(ctx, inst.Name, metav1.DeleteOptions{})
	_ = s.client.Kube.CoreV1().Secrets(s.namespace).Delete(ctx, middlewareSecretName(inst.Name), metav1.DeleteOptions{})
	if withPVC {
		_ = s.client.Kube.CoreV1().PersistentVolumeClaims(s.namespace).Delete(ctx, pvcName(inst.Name), metav1.DeleteOptions{})
	}
}

// generatePassword 用 crypto/rand 生成指定长度的高强度随机密码。
func generatePassword(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = passwordAlphabet[int(buf[i])%len(passwordAlphabet)]
	}
	return string(buf), nil
}
