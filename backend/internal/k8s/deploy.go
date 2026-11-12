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
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"ssedeploy/internal/model"
	"ssedeploy/internal/store"
)

const (
	// deployAppRoot 是仓库代码在 Pod 工作区内的落地目录。
	deployAppRoot = WorkspacePath + "/deployments"
	// deployStateRoot 保存 run.sh / pid / 退出码 / 日志，避免污染应用目录。
	deployStateRoot = WorkspacePath + "/.ssedeploy"
	// deployMaxOutput 单次 exec 输出上限。
	deployMaxOutput = 256 << 10
	// deployFetchTimeout 源码归档下载超时。
	deployFetchTimeout = 3 * time.Minute
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
// 数据库记录 + 在目标 Pod 内拉取代码并执行部署脚本（不再创建新的工作负载）。
type DeployService struct {
	client    *Client
	store     *store.Store
	pods      *PodService
	namespace string
}

// NewDeployService 创建部署业务服务。
func NewDeployService(client *Client, st *store.Store, pods *PodService) *DeployService {
	return &DeployService{client: client, store: st, pods: pods, namespace: client.Namespace}
}

// deployAppDir 返回部署代码在 Pod 内的目录。
func deployAppDir(name string) string { return path.Join(deployAppRoot, name) }

// deployStateDir 返回部署运行状态目录。
func deployStateDir(name string) string { return path.Join(deployStateRoot, name) }

// deployLogPath 返回部署日志路径（提示用户时可读）。
func deployLogPath(name string) string { return path.Join(deployStateDir(name), "deploy.log") }

// List 返回指定 Pod 下的部署列表，同时把 Pod 内的真实运行状态回写到数据库。
func (s *DeployService) List(ctx context.Context, podName string, userID int64, isAdmin bool) ([]model.DeployView, error) {
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
		if d.PodName != podName {
			continue
		}
		status, message := s.syncStatus(ctx, d)
		views = append(views, model.DeployView{
			PodName:      d.PodName,
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
			WebhookPath:  fmt.Sprintf("/api/v1/pods/%s/deployments/%s/webhook", d.PodName, d.Name),
			WebhookToken: webhookTokenFor(d, userID, isAdmin),
			Owner:        ownerDisplay(users[d.OwnerID]),
			MyRole:       deployRole(d.OwnerID, userID, isAdmin),
			CreatedAt:    d.CreatedAt,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].CreatedAt > views[j].CreatedAt })
	return views, nil
}

// Create 创建部署实例：写库 → 在目标 Pod 内拉取代码并启动部署脚本。
func (s *DeployService) Create(ctx context.Context, podName string, req model.CreateDeployRequest, ownerID int64) (*model.Deploy, error) {
	if err := normalizeCreate(&req); err != nil {
		return nil, err
	}
	if _, err := s.pods.runningPod(ctx, podName); err != nil {
		return nil, fmt.Errorf("目标 Pod 当前不可用: %w", err)
	}
	if _, err := s.store.GetDeploy(s.namespace, req.Name); err == nil {
		return nil, store.ErrDuplicate
	} else if err != store.ErrNotFound {
		return nil, err
	}

	d := &model.Deploy{
		Namespace:    s.namespace,
		PodName:      podName,
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
	if err := s.store.CreateDeploy(d); err != nil {
		return nil, err
	}
	if err := s.launch(ctx, d); err != nil {
		// 启动失败时不留下半成品记录，避免用户重复创建
		_ = s.store.DeleteDeploy(s.namespace, d.Name)
		return nil, err
	}
	return d, nil
}

// Update 编辑部署配置；运行中的实例会按新配置重新拉取代码并重启脚本。
func (s *DeployService) Update(ctx context.Context, podName, name string, req model.UpdateDeployRequest, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, podName, name, userID, isAdmin)
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
	if err := s.store.UpdateDeploy(record); err != nil {
		return err
	}

	// 只有正在运行的实例才需要重新执行部署脚本
	if record.Status == model.DeployStatusRunning || record.Status == model.DeployStatusPending {
		return s.restart(ctx, record)
	}
	return nil
}

// Delete 删除部署实例：停止进程、清理 Pod 内目录，再删除记录。
func (s *DeployService) Delete(ctx context.Context, podName, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, podName, name, userID, isAdmin)
	if err != nil {
		return err
	}
	if pod, err := s.pods.runningPod(ctx, record.PodName); err == nil {
		_ = s.stopProcess(ctx, pod, record)
		_ = s.removeFiles(ctx, pod, record.Name)
	}
	return s.store.DeleteDeploy(s.namespace, name)
}

// Start 在 Pod 内启动部署脚本。
func (s *DeployService) Start(ctx context.Context, podName, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, podName, name, userID, isAdmin)
	if err != nil {
		return err
	}
	return s.restart(ctx, record)
}

// Stop 停止 Pod 内的部署进程。
func (s *DeployService) Stop(ctx context.Context, podName, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, podName, name, userID, isAdmin)
	if err != nil {
		return err
	}
	pod, err := s.pods.runningPod(ctx, record.PodName)
	if err != nil {
		// Pod 本身没有运行中的实例，直接标记为已停止
		return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusStopped, "")
	}
	if err := s.stopProcess(ctx, pod, record); err != nil {
		return err
	}
	return s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusStopped, "")
}

// Restart 重新拉取代码并执行部署脚本。
func (s *DeployService) Restart(ctx context.Context, podName, name string, userID int64, isAdmin bool) error {
	record, err := s.access(ctx, podName, name, userID, isAdmin)
	if err != nil {
		return err
	}
	return s.restart(ctx, record)
}

// HandleWebhook 处理 Git 平台推送回调：
// 校验密钥 / 签名 → 判断分支 → 在 Pod 内重新执行部署脚本。
// triggered=false 表示本次推送被忽略（分支不匹配）。
func (s *DeployService) HandleWebhook(ctx context.Context, podName, name, token, signature string, body []byte) (bool, error) {
	record, err := s.store.GetDeploy(s.namespace, name)
	if err != nil {
		return false, err
	}
	if record.PodName != podName {
		return false, store.ErrNotFound
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

	if err := s.restart(ctx, record); err != nil {
		return false, err
	}
	_ = s.store.UpdateDeployStatus(s.namespace, name, model.DeployStatusPending, "Webhook 触发重新部署")
	return true, nil
}

// restart 先停止旧进程，再按当前配置重新拉取代码并启动。
func (s *DeployService) restart(ctx context.Context, d *model.Deploy) error {
	if pod, err := s.pods.runningPod(ctx, d.PodName); err == nil {
		_ = s.stopProcess(ctx, pod, d)
	}
	if err := s.launch(ctx, d); err != nil {
		_ = s.store.UpdateDeployStatus(s.namespace, d.Name, model.DeployStatusFatal, err.Error())
		return err
	}
	return s.store.UpdateDeployStatus(s.namespace, d.Name, model.DeployStatusPending, "")
}

// launch 准备代码 + 写入 run.sh + 后台拉起。
func (s *DeployService) launch(ctx context.Context, d *model.Deploy) error {
	pod, err := s.pods.runningPod(ctx, d.PodName)
	if err != nil {
		return fmt.Errorf("目标 Pod 当前不可用: %w", err)
	}

	hasGit := s.podHasGit(ctx, pod)
	// 容器里没有 git 时，由后端下载公开仓库源码归档并解压进 Pod
	if d.Source == model.DeploySourceRepo && strings.TrimSpace(d.RepoURL) != "" && !hasGit {
		if err := s.syncArchive(ctx, pod, d); err != nil {
			return err
		}
	}

	script := buildRunScript(d, hasGit)
	if err := s.writeScript(ctx, pod, d, script); err != nil {
		return err
	}
	return s.spawn(ctx, pod, d)
}

// podHasGit 检查 Pod 主容器是否安装了 git。
func (s *DeployService) podHasGit(ctx context.Context, pod *corev1.Pod) bool {
	_, err := s.pods.execCapture(ctx, pod, []string{"/bin/sh", "-c", "command -v git >/dev/null 2>&1"}, nil, 1024)
	return err == nil
}

// syncArchive 下载源码归档并流式解压到 Pod 内的应用目录（仅公开仓库）。
func (s *DeployService) syncArchive(ctx context.Context, pod *corev1.Pod, d *model.Deploy) error {
	src := archiveURL(d.RepoURL, d.Branch, d.Token)
	if src == "" {
		return fmt.Errorf("容器内未安装 git，且无法识别仓库 %s 的源码归档地址", d.RepoURL)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, deployFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, src, nil)
	if err != nil {
		return fmt.Errorf("构建源码下载请求失败: %w", err)
	}
	req.Header.Set("User-Agent", "ssedeploy")
	if d.Token != "" {
		req.Header.Set("Authorization", "Bearer "+d.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("下载源码归档失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载源码归档失败：HTTP %d（仓库可能存在或为私有仓库）", resp.StatusCode)
	}
	// Gitee 对匿名归档请求会返回登录/验证网页，这里提前拦截并给出可操作提示
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/html") {
		return fmt.Errorf("下载源码归档失败：仓库返回了网页而不是压缩包，请填写该仓库的访问 Token 后重试")
	}

	appDir := deployAppDir(d.Name)
	script := `mkdir -p "$1" && rm -rf "$1"/* "$1"/.[!.]* 2>/dev/null; tar -xzf - -C "$1" --strip-components=1`
	if _, err := s.pods.execCapture(fetchCtx, pod, []string{"/bin/sh", "-c", script, "sh", appDir}, resp.Body, deployMaxOutput); err != nil {
		return fmt.Errorf("解压源码归档失败: %w", err)
	}
	return nil
}

// writeScript 把 run.sh 写入 Pod 内的状态目录。
func (s *DeployService) writeScript(ctx context.Context, pod *corev1.Pod, d *model.Deploy, script string) error {
	stateDir := deployStateDir(d.Name)
	const cmd = `mkdir -p "$1" && cat > "$2" && chmod +x "$2"`
	_, err := s.pods.execCapture(
		ctx, pod,
		[]string{"/bin/sh", "-c", cmd, "sh", stateDir, path.Join(stateDir, "run.sh")},
		strings.NewReader(script), deployMaxOutput,
	)
	if err != nil {
		return fmt.Errorf("写入部署脚本失败: %w", err)
	}
	return nil
}

// spawn 以独立进程组后台运行 run.sh，避免 HTTP 请求结束后进程被回收。
func (s *DeployService) spawn(ctx context.Context, pod *corev1.Pod, d *model.Deploy) error {
	stateDir := deployStateDir(d.Name)
	script := fmt.Sprintf(`cd %s || exit 1
rm -f exit pid
if command -v setsid >/dev/null 2>&1; then
  nohup setsid /bin/sh ./run.sh >/dev/null 2>&1 < /dev/null &
else
  nohup /bin/sh ./run.sh >/dev/null 2>&1 < /dev/null &
fi
echo started`, shellQuote(stateDir))
	out, err := s.pods.execCapture(ctx, pod, []string{"/bin/sh", "-c", script}, nil, deployMaxOutput)
	if err != nil {
		return fmt.Errorf("启动部署进程失败: %w", err)
	}
	if !strings.Contains(out, "started") {
		return fmt.Errorf("启动部署进程失败")
	}
	return nil
}

// stopProcess 杀掉部署进程组，并写入 stopped 退出标记。
func (s *DeployService) stopProcess(ctx context.Context, pod *corev1.Pod, d *model.Deploy) error {
	script := fmt.Sprintf(`S=%s
mkdir -p "$S"
if [ -f "$S/pid" ]; then
  p=$(cat "$S/pid" 2>/dev/null)
  if [ -n "$p" ] && kill -0 "$p" 2>/dev/null; then
    kill -TERM -"$p" 2>/dev/null || kill -TERM "$p" 2>/dev/null || true
  fi
fi
echo stopped > "$S/exit"
rm -f "$S/pid"
echo ok`, shellQuote(deployStateDir(d.Name)))
	_, err := s.pods.execCapture(ctx, pod, []string{"/bin/sh", "-c", script}, nil, deployMaxOutput)
	if err != nil {
		return fmt.Errorf("停止部署进程失败: %w", err)
	}
	return nil
}

// removeFiles 删除部署在 Pod 内的代码目录与状态目录。
func (s *DeployService) removeFiles(ctx context.Context, pod *corev1.Pod, name string) error {
	script := fmt.Sprintf(`rm -rf %s %s && echo ok`, shellQuote(deployAppDir(name)), shellQuote(deployStateDir(name)))
	_, err := s.pods.execCapture(ctx, pod, []string{"/bin/sh", "-c", script}, nil, deployMaxOutput)
	return err
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

// access 读取部署记录并校验归属与所属 Pod。
func (s *DeployService) access(_ context.Context, podName, name string, userID int64, isAdmin bool) (*model.Deploy, error) {
	record, err := s.store.GetDeploy(s.namespace, name)
	if err != nil {
		return nil, err
	}
	if record.PodName != podName {
		return nil, store.ErrNotFound
	}
	if !isAdmin && record.OwnerID != userID {
		return nil, ErrPodForbidden
	}
	return record, nil
}

// syncStatus 读取 Pod 内真实状态并回写数据库，返回最新状态与补充说明。
func (s *DeployService) syncStatus(ctx context.Context, d *model.Deploy) (string, string) {
	status, message := s.calcStatus(ctx, d)
	if status != "" && (status != d.Status || message != d.Message) {
		_ = s.store.UpdateDeployStatus(s.namespace, d.Name, status, message)
	}
	return status, message
}

// calcStatus 根据 Pod 内的 pid / 退出码文件计算部署状态。
func (s *DeployService) calcStatus(ctx context.Context, d *model.Deploy) (string, string) {
	pod, err := s.pods.runningPod(ctx, d.PodName)
	if err != nil {
		return model.DeployStatusStopped, "Pod 未运行：" + err.Error()
	}
	state, err := s.stateSnapshot(ctx, pod, d.Name)
	if err != nil {
		return model.DeployStatusFatal, "读取部署状态失败：" + err.Error()
	}
	switch {
	case state == "running":
		return model.DeployStatusRunning, ""
	case state == "pending":
		return model.DeployStatusPending, "部署脚本执行中"
	case state == "none":
		if d.Status == model.DeployStatusStopped {
			return model.DeployStatusStopped, ""
		}
		return model.DeployStatusNotFound, "该 Pod 内尚未执行部署脚本"
	case strings.HasPrefix(state, "exit:"):
		code := strings.TrimPrefix(state, "exit:")
		switch {
		case code == "stopped":
			return model.DeployStatusStopped, ""
		case code == "0":
			if d.Type == model.DeployTypeJob {
				return model.DeployStatusSucceeded, ""
			}
			return model.DeployStatusStopped, "部署脚本已退出（退出码 0）"
		default:
			return model.DeployStatusFatal, fmt.Sprintf("部署脚本执行失败（退出码 %s），日志：%s", code, deployLogPath(d.Name))
		}
	}
	return model.DeployStatusPending, ""
}

// stateSnapshot 读取 pid / 退出码，返回 running / pending / none / exit:<code>。
func (s *DeployService) stateSnapshot(ctx context.Context, pod *corev1.Pod, name string) (string, error) {
	script := fmt.Sprintf(`S=%s
if [ -f "$S/pid" ]; then
  p=$(cat "$S/pid" 2>/dev/null)
  if [ -n "$p" ] && kill -0 "$p" 2>/dev/null; then echo running; exit 0; fi
fi
if [ -f "$S/exit" ]; then
  echo "exit:$(cat "$S/exit" 2>/dev/null)"
  exit 0
fi
if [ -f "$S/run.sh" ]; then echo pending; exit 0; fi
echo none`, shellQuote(deployStateDir(name)))
	out, err := s.pods.execCapture(ctx, pod, []string{"/bin/sh", "-c", script}, nil, deployMaxOutput)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// buildRunScript 生成 Pod 内的部署脚本。
//
// 脚本职责：拉取代码（git 优先）→ 进入子目录 → 执行 deploy.sh / deploy/deploy.sh / start.sh。
// 常驻服务的脚本需在前台持续运行；脚本退出后状态会变为「已停止」，非 0 退出码为 fatal。
func buildRunScript(d *model.Deploy, hasGit bool) string {
	appDir := deployAppDir(d.Name)
	stateDir := deployStateDir(d.Name)
	authRepo := authRepoURL(d.RepoURL, d.Token)

	fetchBlock := ""
	if d.Source == model.DeploySourceRepo && strings.TrimSpace(d.RepoURL) != "" {
		if hasGit {
			fetchBlock = `echo "[ssedeploy] 拉取仓库 $SSD_REPO_URL 分支 $SSD_BRANCH"
if [ -d "$APP/.git" ]; then
  git -C "$APP" remote set-url origin "$AUTH_REPO" || true
  if ! git -C "$APP" fetch --depth 1 origin "$SSD_BRANCH"; then echo "[ssedeploy] git fetch 失败"; exit 1; fi
  if ! git -C "$APP" reset --hard FETCH_HEAD; then echo "[ssedeploy] git reset 失败"; exit 1; fi
  git -C "$APP" clean -fdx || true
else
  rm -rf "$APP"
  mkdir -p "$(dirname "$APP")"
  if ! git clone --depth 1 --branch "$SSD_BRANCH" "$AUTH_REPO" "$APP"; then
    echo "[ssedeploy] git clone 失败：请检查仓库地址、分支与访问 Token"
    exit 1
  fi
fi
echo "[ssedeploy] 代码已更新到 $APP"
`
		} else {
			fetchBlock = `echo "[ssedeploy] 容器内未安装 git，使用平台下载的源码归档"
`
		}
	}

	return fmt.Sprintf(`#!/bin/sh
set -u
APP=%s
STATE=%s
LOG="$STATE/deploy.log"
EXIT="$STATE/exit"
PID="$STATE/pid"
AUTH_REPO=%s

mkdir -p "$STATE" "$APP"
rm -f "$EXIT" "$PID"
echo $$ > "$PID"
trap 'code=$?; echo "$code" > "$EXIT"; rm -f "$PID"' EXIT
: > "$LOG"
exec >>"$LOG" 2>&1

export SSD_NAME=%s
export SSD_POD=%s
export SSD_REPO_URL=%s
export SSD_BRANCH=%s
export SSD_SUB_DIR=%s
export SSD_TYPE=%s
export SSD_GPU=%s
export SSD_SCHEDULE_TYPE=%s
export SSD_HEALTH_PATH=%s
export SSD_TIMEOUT_SEC=%s
export SSD_APP_DIR="$APP"
export SSD_STATE_DIR="$STATE"

echo "[ssedeploy] $(date '+%%Y-%%m-%%d %%H:%%M:%%S') 开始部署 $SSD_NAME（Pod $SSD_POD）"
%s
RUN_DIR="$APP"
if [ -n "${SSD_SUB_DIR:-}" ]; then RUN_DIR="$APP/$SSD_SUB_DIR"; fi
cd "$RUN_DIR" || { echo "[ssedeploy] 代码目录不存在：$RUN_DIR"; exit 1; }

SCRIPT=""
for f in "$RUN_DIR/deploy.sh" "$RUN_DIR/deploy/deploy.sh" "$RUN_DIR/start.sh"; do
  if [ -f "$f" ]; then SCRIPT="$f"; break; fi
done
if [ -z "$SCRIPT" ]; then
  echo "[ssedeploy] 未找到部署脚本：已尝试 deploy.sh、deploy/deploy.sh、start.sh"
  exit 2
fi

echo "[ssedeploy] 执行部署脚本 $SCRIPT"
if [ -x "$SCRIPT" ]; then "$SCRIPT"; else /bin/sh "$SCRIPT"; fi
code=$?
echo "[ssedeploy] 部署脚本退出，退出码 $code"
exit "$code"
`,
		shellQuote(appDir),
		shellQuote(stateDir),
		shellQuote(authRepo),
		shellQuote(d.Name),
		shellQuote(d.PodName),
		shellQuote(d.RepoURL),
		shellQuote(d.Branch),
		shellQuote(d.SubDir),
		shellQuote(d.Type),
		shellQuote(d.GPU),
		shellQuote(d.ScheduleType),
		shellQuote(d.HealthPath),
		shellQuote(fmt.Sprintf("%d", d.TimeoutSec)),
		fetchBlock,
	)
}

// authRepoURL 把访问 Token 注入 HTTPS 仓库地址（私有仓库）。
func authRepoURL(repoURL, token string) string {
	raw := strings.TrimSpace(repoURL)
	if raw == "" || strings.TrimSpace(token) == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" {
		return raw
	}
	parsed.User = url.UserPassword("oauth2", strings.TrimSpace(token))
	return parsed.String()
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

// archiveURL 生成源码归档下载地址（GitHub / Gitee）。
// 容器内没有 git 时由后端下载并解压到 Pod，公开仓库无需任何客户端。
func archiveURL(repoURL, branch, token string) string {
	owner, repo, host := splitRepo(repoURL)
	if owner == "" || repo == "" {
		return ""
	}
	if branch == "" {
		branch = "main"
	}
	switch {
	case strings.Contains(host, "github.com"):
		return fmt.Sprintf("https://api.github.com/repos/%s/%s/tarball/%s", owner, repo, url.PathEscape(branch))
	case strings.Contains(host, "gitee.com"):
		// 有 Token 时走 API v5（公开仓库同样要求令牌），否则退回网页归档地址
		if token = strings.TrimSpace(token); token != "" {
			return fmt.Sprintf("https://gitee.com/api/v5/repos/%s/%s/tarball?ref=%s&access_token=%s",
				owner, repo, url.QueryEscape(branch), url.QueryEscape(token))
		}
		return fmt.Sprintf("https://gitee.com/%s/%s/repository/archive/%s.tar.gz", owner, repo, url.PathEscape(branch))
	default:
		return ""
	}
}

// splitRepo 从仓库地址中解析 owner / repo / host。
func splitRepo(repoURL string) (owner, repo, host string) {
	raw := strings.TrimSuffix(strings.TrimSpace(repoURL), "/")
	raw = strings.TrimSuffix(raw, ".git")
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", "", ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", ""
	}
	return parts[0], parts[1], strings.ToLower(parsed.Host)
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
