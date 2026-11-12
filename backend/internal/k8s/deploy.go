package k8s

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

const (
	// deployRuntimeImage 是部署实例的占位运行时镜像，随 K3s 离线包一起预加载。
	// 平台尚未接入 Git 构建流水线：实例会先尝试用 busybox 拉取仓库源码归档，
	// 再以 busybox httpd 作为占位服务对外提供健康检查端点。
	deployRuntimeImage = "rancher/mirrored-library-busybox:1.37.0"
	// deployKindLabel 标记该工作负载由「部署」模块创建，便于与普通 Pod 区分。
	deployKindLabel = "ssedeploy.io/kind"
	// deployKindValue 部署模块的工作负载取值。
	deployKindValue = "deploy"
)

// deployNameRe 与 Pod 名称规则一致，遵循 DNS-1123。
var deployNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// Webhook 相关错误，handler 层据此返回对应状态码。
var (
	// ErrWebhookForbidden 回调密钥 / 签名校验失败
	ErrWebhookForbidden = errors.New("Webhook 校验失败：token 或签名不正确")
	// ErrWebhookDisabled 该部署未开启 push 自动部署
	ErrWebhookDisabled = errors.New("该部署未开启 push 自动部署")
)

// DeployService 负责部署实例的业务编排：
// 数据库记录 + K8s 工作负载（常驻服务 -> Deployment，一次性脚本 -> Job）。
type DeployService struct {
	client    *Client
	store     *store.Store
	namespace string
}

// NewDeployService 创建部署业务服务。
func NewDeployService(client *Client, st *store.Store) *DeployService {
	return &DeployService{client: client, store: st, namespace: client.Namespace}
}

// List 返回部署列表：管理员可见全部，普通用户仅本人创建。
// 同时把集群里的真实运行状态回写到数据库。
func (s *DeployService) List(ctx context.Context, userID int64, isAdmin bool) ([]model.DeployView, error) {
	records, err := s.store.ListDeploys(s.namespace)
	if err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers()
	if err != nil {
		return nil, err
	}

	views := make([]model.DeployView, 0, len(records))
	for _, d := range records {
		if !isAdmin && d.OwnerID != userID {
			continue
		}
		status, message := s.syncStatus(ctx, d)
		views = append(views, model.DeployView{
			Name:         d.Name,
			Status:       status,
			StatusText:   model.DeployStatusText(status),
			Message:      message,
			Source:       d.Source,
			SourceText:   model.DeploySourceText(d.Source),
			RepoURL:      d.RepoURL,
			Branch:       d.Branch,
			SubDir:       d.SubDir,
			Type:         d.Type,
			TypeText:     model.DeployTypeText(d.Type),
			ScheduleType: d.ScheduleType,
			GPU:          d.GPU,
			HealthPath:   d.HealthPath,
			TimeoutSec:   d.TimeoutSec,
			AutoDeploy:   d.AutoDeploy,
			WebhookPath:  fmt.Sprintf("/api/v1/deployments/%s/webhook", d.Name),
			WebhookToken: webhookTokenFor(d, userID, isAdmin),
			Owner:        ownerDisplay(users[d.OwnerID]),
			MyRole:       deployRole(d.OwnerID, userID, isAdmin),
			CreatedAt:    d.CreatedAt,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt > views[j].CreatedAt })
	return views, nil
}

// Create 创建部署实例：校验参数 → 拉起工作负载 → 写入数据库记录。
func (s *DeployService) Create(ctx context.Context, req model.CreateDeployRequest, ownerID int64) (*model.Deploy, error) {
	if err := normalizeCreate(&req); err != nil {
		return nil, err
	}
	if _, err := s.store.GetDeploy(s.namespace, req.Name); err == nil {
		return nil, store.ErrDuplicate
	} else if err != store.ErrNotFound {
		return nil, err
	}

	s.client.ensureNamespace(ctx)

	d := &model.Deploy{
		Namespace:    s.namespace,
		Name:         req.Name,
		OwnerID:      ownerID,
		Source:       req.Source,
		RepoURL:      req.RepoURL,
		Branch:       req.Branch,
		SubDir:       req.SubDir,
		Type:         req.Type,
		ScheduleType: req.ScheduleType,
		GPU:          req.GPU,
		Token:        req.Token,
		HealthPath:   req.HealthPath,
		TimeoutSec:   req.TimeoutSec,
		AutoDeploy:   req.AutoDeploy,
		// 每次创建都生成独立的回调密钥，用于校验 Git 平台推送
		WebhookSecret: newWebhookSecret(),
		Status:        model.DeployStatusPending,
	}
	if err := s.createWorkload(ctx, d, 1); err != nil {
		return nil, err
	}
	if err := s.store.CreateDeploy(d); err != nil {
		// 数据库写入失败时回收刚刚创建的工作负载，避免留下孤儿资源
		_ = s.deleteWorkload(context.Background(), d)
		return nil, err
	}
	return d, nil
}

// Update 编辑部署配置；工作负载会按新配置重建，运行中的实例保持运行。
func (s *DeployService) Update(ctx context.Context, name string, req model.UpdateDeployRequest, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, name, userID, isAdmin)
	if err != nil {
		return err
	}

	record.RepoURL = strings.TrimSpace(req.RepoURL)
	record.Branch = defaultString(strings.TrimSpace(req.Branch), "main")
	record.SubDir = strings.Trim(strings.TrimSpace(req.SubDir), "/")
	record.Type = defaultString(req.Type, model.DeployTypeService)
	record.ScheduleType = defaultString(req.ScheduleType, "inference")
	record.GPU = strings.TrimSpace(req.GPU)
	record.HealthPath = strings.TrimSpace(req.HealthPath)
	record.TimeoutSec = req.TimeoutSec
	record.AutoDeploy = req.AutoDeploy
	// 前端留空表示不修改已有令牌
	if strings.TrimSpace(req.Token) != "" {
		record.Token = strings.TrimSpace(req.Token)
	}
	if record.TimeoutSec <= 0 {
		record.TimeoutSec = 5
	}
	if err := validateDeploy(record); err != nil {
		return err
	}

	replicas := int32(1)
	if record.Status == model.DeployStatusStopped {
		replicas = 0
	}
	// 类型可能变化（Deployment <-> Job），先清理旧工作负载再按新配置重建
	if err := s.deleteWorkload(ctx, record); err != nil {
		return err
	}
	if err := s.createWorkload(ctx, record, replicas); err != nil {
		return err
	}
	return s.store.UpdateDeploy(record)
}

// Delete 删除部署实例及其集群工作负载。
func (s *DeployService) Delete(ctx context.Context, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, name, userID, isAdmin)
	if err != nil {
		return err
	}
	if err := s.deleteWorkload(ctx, record); err != nil {
		return err
	}
	return s.store.DeleteDeploy(s.namespace, name)
}

// Start 启动部署实例：常驻服务扩容到 1，一次性脚本重建 Job。
func (s *DeployService) Start(ctx context.Context, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, name, userID, isAdmin)
	if err != nil {
		return err
	}
	if record.Type == model.DeployTypeJob {
		_ = s.deleteWorkload(ctx, record)
		if err := s.createWorkload(ctx, record, 1); err != nil {
			return err
		}
		return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "")
	}

	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err := s.createWorkload(ctx, record, 1); err != nil {
			return err
		}
		return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "")
	}
	if err != nil {
		return err
	}
	replicas := int32(1)
	dep.Spec.Replicas = &replicas
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("启动部署失败: %w", err)
	}
	return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "")
}

// Stop 停止部署实例：常驻服务缩容到 0，一次性脚本直接删除 Job。
func (s *DeployService) Stop(ctx context.Context, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, name, userID, isAdmin)
	if err != nil {
		return err
	}
	if record.Type == model.DeployTypeJob {
		if err := s.deleteWorkload(ctx, record); err != nil {
			return err
		}
		return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusStopped, "")
	}

	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusStopped, "")
	}
	if err != nil {
		return err
	}
	replicas := int32(0)
	dep.Spec.Replicas = &replicas
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("停止部署失败: %w", err)
	}
	return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusStopped, "")
}

// Restart 重新拉起部署实例。
func (s *DeployService) Restart(ctx context.Context, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, name, userID, isAdmin)
	if err != nil {
		return err
	}
	if record.Type == model.DeployTypeJob {
		_ = s.deleteWorkload(ctx, record)
		if err := s.createWorkload(ctx, record, 1); err != nil {
			return err
		}
		return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "")
	}

	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if err := s.createWorkload(ctx, record, 1); err != nil {
			return err
		}
		return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "")
	}
	if err != nil {
		return err
	}
	if dep.Spec.Template.Annotations == nil {
		dep.Spec.Template.Annotations = map[string]string{}
	}
	// 与 Pod 重启一致：修改模板注解触发 Recreate 重建
	dep.Spec.Template.Annotations["ssedeploy.io/restarted-at"] = timeNow()
	if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("重启部署失败: %w", err)
	}
	return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "")
}

// HandleWebhook 处理 Git 平台推送回调：
// 校验密钥 / 签名 → 判断分支 → 重建工作负载。
// triggered=false 表示本次推送被忽略（分支不匹配）。
func (s *DeployService) HandleWebhook(ctx context.Context, name, token, signature string, body []byte) (bool, error) {
	record, err := s.store.GetDeploy(s.namespace, name)
	if err != nil {
		return false, err
	}
	if record.WebhookSecret == "" || !record.AutoDeploy {
		return false, ErrWebhookDisabled
	}
	if !verifyWebhook(record.WebhookSecret, token, signature, body) {
		return false, ErrWebhookForbidden
	}
	// 推送分支与部署配置的分支不一致时忽略本次推送
	if pushed := refBranch(body); pushed != "" && record.Branch != "" && pushed != record.Branch {
		return false, nil
	}

	if err := s.deleteWorkload(ctx, record); err != nil {
		return false, err
	}
	if err := s.createWorkload(ctx, record, 1); err != nil {
		return false, err
	}
	_ = s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "Webhook 触发重新部署")
	return true, nil
}

// verifyWebhook 支持三种校验方式：
//  1. URL 查询参数 token=xxx
//  2. Gitee 的 X-Gitee-Token 请求头
//  3. GitHub 的 X-Hub-Signature-256 HMAC-SHA256 签名
func verifyWebhook(secret, token, signature string, body []byte) bool {
	if token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1 {
		return true
	}
	if strings.HasPrefix(signature, "sha256=") {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(expected), []byte(signature))
	}
	return false
}

// refBranch 从推送 payload 中提取分支名（refs/heads/main -> main）。
func refBranch(body []byte) string {
	var payload struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimPrefix(payload.Ref, "refs/heads/")
}

// newWebhookSecret 生成 16 字节随机回调密钥。
func newWebhookSecret() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// webhookTokenFor 仅负责人与管理员可见回调密钥。
func webhookTokenFor(d *model.Deploy, userID int64, isAdmin bool) string {
	if isAdmin || d.OwnerID == userID {
		return d.WebhookSecret
	}
	return ""
}

// access 读取部署记录并校验归属：管理员可操作全部，普通用户仅限本人。
func (s *DeployService) access(_ context.Context, name string, userID int64, isAdmin bool) (*model.Deploy, error) {
	record, err := s.store.GetDeploy(s.namespace, name)
	if err != nil {
		return nil, err
	}
	if !isAdmin && record.OwnerID != userID {
		return nil, ErrPodForbidden
	}
	return record, nil
}

// syncStatus 读取集群真实状态并回写数据库，返回最新状态与失败原因。
func (s *DeployService) syncStatus(ctx context.Context, d *model.Deploy) (string, string) {
	status, message := s.calcStatus(ctx, d)
	if status != "" && (status != d.Status || message != d.Message) {
		_ = s.store.UpdateDeployStatus(s.namespace, d.Name, status, message)
	}
	return status, message
}

// calcStatus 根据工作负载与 Pod 状态计算部署状态。
func (s *DeployService) calcStatus(ctx context.Context, d *model.Deploy) (string, string) {
	if d.Type == model.DeployTypeJob {
		job, err := s.client.Kube.BatchV1().Jobs(s.namespace).Get(ctx, d.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			if d.Status == model.DeployStatusStopped {
				return model.DeployStatusStopped, ""
			}
			return model.DeployStatusNotFound, "Job 已被删除"
		}
		if err != nil {
			return d.Status, err.Error()
		}
		if job.Status.Succeeded > 0 {
			return model.DeployStatusSucceeded, ""
		}
		if job.Status.Failed > 0 && job.Status.Active == 0 {
			return model.DeployStatusFatal, "一次性脚本执行失败"
		}
		return model.DeployStatusRunning, ""
	}

	dep, err := s.client.Kube.AppsV1().Deployments(s.namespace).Get(ctx, d.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return model.DeployStatusNotFound, "Deployment 已被删除"
	}
	if err != nil {
		return d.Status, err.Error()
	}
	if dep.Spec.Replicas != nil && *dep.Spec.Replicas == 0 {
		return model.DeployStatusStopped, ""
	}

	pods, err := s.client.Kube.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: appLabel + "=" + d.Name + "," + ManagedLabel + "=true",
	})
	if err == nil {
		for i := range pods.Items {
			if reason := podFailReason(&pods.Items[i]); reason != "" {
				return model.DeployStatusFatal, "服务启动失败：" + reason
			}
		}
	}
	if dep.Status.ReadyReplicas > 0 {
		return model.DeployStatusRunning, ""
	}
	return model.DeployStatusPending, "容器启动中"
}

// createWorkload 按部署类型创建 Deployment 或 Job。
func (s *DeployService) createWorkload(ctx context.Context, d *model.Deploy, replicas int32) error {
	labels := map[string]string{
		appLabel:        d.Name,
		ManagedLabel:    "true",
		deployKindLabel: deployKindValue,
	}
	annotations := map[string]string{
		"ssedeploy.io/owner-id":        fmt.Sprintf("%d", d.OwnerID),
		"ssedeploy.io/created-by":      "ssedeploy",
		"ssedeploy.io/deploy-source":   d.Source,
		"ssedeploy.io/deploy-repo":     d.RepoURL,
		"ssedeploy.io/deploy-branch":   d.Branch,
		"ssedeploy.io/deploy-sub-dir":  d.SubDir,
		"ssedeploy.io/deploy-type":     d.Type,
		"ssedeploy.io/deploy-schedule": d.ScheduleType,
		"ssedeploy.io/deploy-gpu":      d.GPU,
	}
	container := s.buildContainer(d)
	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
		Volumes: []corev1.Volume{
			{Name: "workspace", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		},
	}

	switch d.Type {
	case model.DeployTypeJob:
		backoff := int32(1)
		job := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: s.namespace, Labels: labels, Annotations: annotations},
			Spec: batchv1.JobSpec{
				BackoffLimit: &backoff,
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec:       withRestartPolicy(podSpec, corev1.RestartPolicyNever),
				},
			},
		}
		if _, err := s.client.Kube.BatchV1().Jobs(s.namespace).Create(ctx, job, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("创建一次性脚本失败: %w", err)
		}
		return nil
	default:
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: d.Name, Namespace: s.namespace, Labels: labels, Annotations: annotations},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicas,
				Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{appLabel: d.Name}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: labels},
					Spec:       podSpec,
				},
			},
		}
		if _, err := s.client.Kube.AppsV1().Deployments(s.namespace).Create(ctx, dep, metav1.CreateOptions{}); err != nil {
			return fmt.Errorf("创建部署失败: %w", err)
		}
		return nil
	}
}

// buildContainer 构造部署实例的容器：拉取源码归档（可选）+ 占位运行时。
func (s *DeployService) buildContainer(d *model.Deploy) corev1.Container {
	srcURL := archiveURL(d.RepoURL, d.Branch)
	script := serviceScript
	if d.Type == model.DeployTypeJob {
		script = jobScript
	}

	env := []corev1.EnvVar{
		{Name: "SSD_NAME", Value: d.Name},
		{Name: "SSD_REPO_URL", Value: d.RepoURL},
		{Name: "SSD_BRANCH", Value: d.Branch},
		{Name: "SSD_SUB_DIR", Value: d.SubDir},
		{Name: "SSD_SRC_URL", Value: srcURL},
		{Name: "SSD_HEALTH_PATH", Value: d.HealthPath},
		{Name: "SSD_GPU", Value: d.GPU},
		{Name: "SSD_SCHEDULE_TYPE", Value: d.ScheduleType},
	}

	container := corev1.Container{
		Name:            mainContainer,
		Image:           deployRuntimeImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-c", script},
		Env:             env,
		WorkingDir:      "/workspace",
		Ports:           []corev1.ContainerPort{{Name: "http", ContainerPort: 8080}},
		VolumeMounts:    []corev1.VolumeMount{{Name: "workspace", MountPath: "/workspace"}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resourceMustParse("100m"),
				corev1.ResourceMemory: resourceMustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resourceMustParse("1"),
				corev1.ResourceMemory: resourceMustParse("1Gi"),
			},
		},
	}

	// 只有常驻服务且配置了健康检查路径时才注入探针：
	// 探针失败会被 K8s 判定为不健康并自动重启容器。
	if d.Type != model.DeployTypeJob && strings.TrimSpace(d.HealthPath) != "" {
		timeout := int32(d.TimeoutSec)
		if timeout <= 0 {
			timeout = 5
		}
		probe := &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: d.HealthPath,
					Port: intstr.FromString("http"),
				},
			},
			TimeoutSeconds:   timeout,
			PeriodSeconds:    10,
			FailureThreshold: 3,
		}
		container.ReadinessProbe = probe
		container.LivenessProbe = probe.DeepCopy()
	}
	return container
}

// deleteWorkload 删除部署对应的 Deployment / Job，不存在时视为成功。
func (s *DeployService) deleteWorkload(ctx context.Context, d *model.Deploy) error {
	policy := metav1.DeletePropagationBackground
	if d.Type == model.DeployTypeJob {
		err := s.client.Kube.BatchV1().Jobs(s.namespace).Delete(ctx, d.Name, metav1.DeleteOptions{PropagationPolicy: &policy})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("删除一次性脚本失败: %w", err)
		}
		return nil
	}
	err := s.client.Kube.AppsV1().Deployments(s.namespace).Delete(ctx, d.Name, metav1.DeleteOptions{PropagationPolicy: &policy})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("删除部署失败: %w", err)
	}
	return nil
}

// normalizeCreate 填充默认值并校验创建参数。
func normalizeCreate(req *model.CreateDeployRequest) error {
	req.Source = defaultString(req.Source, model.DeploySourceRepo)
	req.Type = defaultString(req.Type, model.DeployTypeService)
	req.ScheduleType = defaultString(req.ScheduleType, "inference")
	req.Branch = defaultString(strings.TrimSpace(req.Branch), "main")
	req.RepoURL = strings.TrimSpace(req.RepoURL)
	req.SubDir = strings.Trim(strings.TrimSpace(req.SubDir), "/")
	req.GPU = strings.TrimSpace(req.GPU)
	req.HealthPath = strings.TrimSpace(req.HealthPath)
	if req.HealthPath != "" && !strings.HasPrefix(req.HealthPath, "/") {
		req.HealthPath = "/" + req.HealthPath
	}
	if req.TimeoutSec <= 0 {
		req.TimeoutSec = 5
	}
	// 名称留空时使用仓库名
	if strings.TrimSpace(req.Name) == "" {
		req.Name = repoName(req.RepoURL)
	}
	req.Name = sanitizeName(req.Name)

	d := &model.Deploy{
		Name:         req.Name,
		Source:       req.Source,
		RepoURL:      req.RepoURL,
		Branch:       req.Branch,
		Type:         req.Type,
		ScheduleType: req.ScheduleType,
		HealthPath:   req.HealthPath,
		TimeoutSec:   req.TimeoutSec,
	}
	return validateDeploy(d)
}

// validateDeploy 校验必填项与资源名称规范。
func validateDeploy(d *model.Deploy) error {
	if d.Name == "" {
		return fmt.Errorf("部署名称不能为空：请填写名称或仓库地址")
	}
	if !deployNameRe.MatchString(d.Name) || len(d.Name) > 45 {
		return fmt.Errorf("部署名称 %s 不合法：只能包含小写字母、数字和中划线，且不超过 45 个字符", d.Name)
	}
	if d.Source == model.DeploySourceRepo && strings.TrimSpace(d.RepoURL) == "" {
		return fmt.Errorf("仓库地址不能为空")
	}
	if d.Type != model.DeployTypeService && d.Type != model.DeployTypeJob {
		return fmt.Errorf("部署类型不合法")
	}
	if d.HealthPath != "" && !strings.HasPrefix(d.HealthPath, "/") {
		return fmt.Errorf("健康检查路径必须以 / 开头")
	}
	return nil
}

// archiveURL 根据仓库地址生成源码归档下载地址。
// 公开的 GitHub / Gitee 仓库无需 git 客户端即可通过 busybox wget 拉取源码。
func archiveURL(repoURL, branch string) string {
	raw := strings.TrimSuffix(strings.TrimSpace(repoURL), "/")
	raw = strings.TrimSuffix(raw, ".git")
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	host := strings.ToLower(parsed.Host)
	switch {
	case strings.Contains(host, "github.com"):
		return fmt.Sprintf("%s/archive/refs/heads/%s.tar.gz", raw, branch)
	case strings.Contains(host, "gitee.com"):
		return fmt.Sprintf("%s/repository/archive/%s.tar.gz", raw, branch)
	default:
		return ""
	}
}

// repoName 从仓库地址中提取仓库名，作为部署名称的默认值。
func repoName(repoURL string) string {
	raw := strings.TrimSuffix(strings.TrimSpace(repoURL), "/")
	raw = strings.TrimSuffix(raw, ".git")
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "/")
	return parts[len(parts)-1]
}

// sanitizeName 把仓库名转成符合 DNS-1123 的资源名称。
func sanitizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_' || r == '.' || r == ' ':
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	if len(result) > 45 {
		result = strings.Trim(result[:45], "-")
	}
	return result
}

// deployRole 计算当前用户相对部署实例的角色。
func deployRole(ownerID, userID int64, isAdmin bool) string {
	switch {
	case isAdmin:
		return "管理员"
	case ownerID == userID:
		return "负责人"
	default:
		return "协作者"
	}
}

// ownerDisplay 取用户展示名。
func ownerDisplay(u *model.User) string {
	if u == nil {
		return "-"
	}
	return u.Display()
}

// defaultString 返回第一个非空值。
func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// podFailReason 提取 Pod 不可用的原因，未失败时返回空串。
func podFailReason(p *corev1.Pod) string {
	if p.Status.Phase == corev1.PodFailed {
		if p.Status.Message != "" {
			return p.Status.Message
		}
		return "Pod 已失败"
	}
	check := func(statuses []corev1.ContainerStatus) string {
		for _, st := range statuses {
			if st.State.Waiting == nil {
				continue
			}
			switch st.State.Waiting.Reason {
			case "ImagePullBackOff", "ErrImagePull", "CrashLoopBackOff", "CreateContainerConfigError", "InvalidImageName":
				return st.State.Waiting.Reason
			}
		}
		return ""
	}
	if reason := check(p.Status.InitContainerStatuses); reason != "" {
		return reason
	}
	return check(p.Status.ContainerStatuses)
}

// withRestartPolicy 返回设置了重启策略的 PodSpec 副本。
func withRestartPolicy(spec corev1.PodSpec, policy corev1.RestartPolicy) corev1.PodSpec {
	spec.RestartPolicy = policy
	return spec
}

// resourceMustParse 解析资源用量，失败时返回 0 值（不会触发 panic）。
func resourceMustParse(value string) resource.Quantity {
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}
	}
	return q
}

// timeNow 返回 RFC3339 时间戳，用于触发重启。
func timeNow() string {
	return time.Now().Format(time.RFC3339)
}

// 说明：部署实例运行的是平台内置占位运行时。
// 平台尚未接入 Git 构建流水线，因此容器会：
//  1. 用 busybox wget 下载 GitHub / Gitee 的源码归档并解压到 /workspace（公开仓库）；
//  2. 以 busybox httpd 监听 8080，提供 / 与健康检查路径，模拟服务的持续运行。
//
// serviceScript 常驻服务启动脚本。
const serviceScript = `set -u
echo "[ssedeploy] 部署 ${SSD_NAME} 启动"
echo "[ssedeploy] 仓库=${SSD_REPO_URL:-本地} 分支=${SSD_BRANCH} 子目录=${SSD_SUB_DIR:-/}"
mkdir -p /workspace /www
echo ok > /www/index.html
if [ -n "${SSD_HEALTH_PATH:-}" ]; then
  mkdir -p "/www$(dirname "$SSD_HEALTH_PATH")"
  echo ok > "/www${SSD_HEALTH_PATH}"
fi
if [ -n "${SSD_SRC_URL:-}" ]; then
  # 源码拉取放到后台，避免外网不可达时阻塞服务启动
  (
    if wget -q -T 20 -t 1 -O /tmp/src.tar.gz "$SSD_SRC_URL"; then
      if tar -xzf /tmp/src.tar.gz -C /workspace --strip-components=1 2>/dev/null; then
        echo "[ssedeploy] 源码已解压到 /workspace"
      else
        echo "[ssedeploy] 源码解压失败，继续使用占位运行时"
      fi
    else
      echo "[ssedeploy] 源码拉取失败（私有仓库或网络不可达），继续使用占位运行时"
    fi
  ) &
fi
echo "[ssedeploy] 占位运行时监听 8080"
exec httpd -f -p 8080 -h /www
`

// jobScript 一次性脚本启动脚本：执行完成后容器退出。
const jobScript = `set -u
echo "[ssedeploy] 一次性脚本 ${SSD_NAME} 开始执行"
echo "[ssedeploy] 仓库=${SSD_REPO_URL:-本地} 分支=${SSD_BRANCH} 子目录=${SSD_SUB_DIR:-/}"
mkdir -p /workspace
if [ -n "${SSD_SRC_URL:-}" ]; then
  if wget -q -T 20 -t 1 -O /tmp/src.tar.gz "$SSD_SRC_URL"; then
    tar -xzf /tmp/src.tar.gz -C /workspace --strip-components=1 2>/dev/null || true
  else
    echo "[ssedeploy] 源码拉取失败，跳过"
  fi
fi
ls -la /workspace || true
echo "[ssedeploy] 一次性脚本执行完成"
exit 0
`
