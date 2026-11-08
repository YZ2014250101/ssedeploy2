# SSEDeploy · 轻量化云原生部署平台（Phase 1：Pod 模块）

对标 YatTerra 的轻量部署控制台。第一阶段只实现 **Pod 模块**，侧边栏其余菜单均为 UI 占位（点击提示「该模块开发中」）。

- 后端：Go + Gin + client-go，RESTful 接口，SQLite 持久化
- 前端：React 18 + TypeScript + Vite + TailwindCSS + shadcn/ui 风格组件
- 集群交互：Deployment + PVC 管理业务 "Pod"，对用户屏蔽 Deployment 概念

## 1. 目录结构

```
ssedeploy2/
├── backend/                     # Go 后端
│   ├── cmd/server/main.go       # 入口：初始化 DB / K8s / 路由 / 指标采样
│   ├── internal/
│   │   ├── config/              # 环境变量配置
│   │   ├── model/               # User / Pod / Metric / Event 数据模型
│   │   ├── store/               # SQLite 访问层（users / pod_metas / metric_samples）
│   │   ├── auth/                # JWT 签发与解析
│   │   ├── middleware/          # 鉴权中间件（Bearer 或 ?token=）
│   │   ├── k8s/                 # client-go 封装：Pod 生命周期、日志、exec、指标、事件
│   │   ├── handler/             # HTTP 处理器（含 WebSSH / 日志 WebSocket）
│   │   └── router/              # 路由与 CORS
│   ├── data/                    # 本地 SQLite 文件（不纳入镜像）
│   └── Dockerfile
├── frontend/                    # React 前端
│   ├── src/
│   │   ├── api/                 # axios 封装与接口定义
│   │   ├── components/ui/       # shadcn 风格基础组件
│   │   ├── components/layout/   # 侧边栏 + 顶部导航
│   │   ├── components/pod/      # 统计卡、表格、卡片、创建弹窗
│   │   ├── pages/               # 登录 / Pod 列表 / Pod 详情
│   │   ├── pages/pod-tabs/      # 监控 / 终端 / 日志 / 设置
│   │   └── lib/                 # 鉴权存储、格式化工具
│   ├── nginx.conf               # SPA 回退 + /api 反代 + WebSocket 升级
│   └── Dockerfile
├── deploy/k8s/rbac.yaml         # 命名空间 + ServiceAccount + RBAC（集群内运行用）
└── docker-compose.yml           # 一键启动（frontend + backend）
```

## 2. 前置依赖

| 依赖 | 说明 |
| --- | --- |
| Kubernetes 集群 | 1.24+，本地推荐 k3d / kind / Docker Desktop |
| metrics-server | 监控 tab 的 CPU/内存数据来源，必须就绪 |
| 默认 StorageClass | 创建 Pod 时会创建 PVC，需有可动态供给的 StorageClass（k3d/kind 自带 local-path） |

### 2.1 准备本地集群（k3d 示例）

```bash
k3d cluster create mycluster --agents 1
kubectl cluster-info
```

### 2.2 安装 metrics-server

```bash
# 官方清单（把 --kubelet-insecure-tls 打开以适配 k3d/kind 自签证书）
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
kubectl patch deployment metrics-server -n kube-system --type=json \
  -p='[{"op":"add","path":"/spec/template/spec/containers/0/args/-","value":"--kubelet-insecure-tls"}]'
kubectl rollout status deployment/metrics-server -n kube-system
kubectl top pods -n kube-system   # 能返回数据说明就绪
```

> metrics-server 未就绪时，监控 tab 会显示「暂无监控数据」，其余功能不受影响。

## 3. 本地开发启动

### 3.1 启动后端

```bash
cd backend
go mod download

# Windows PowerShell
$env:SSD_KUBECONFIG="$env:USERPROFILE\.kube\config"
$env:SSD_K8S_NAMESPACE="ssedeploy"
$env:SSD_DB_PATH=".\data\ssedeploy.db"
go run ./cmd/server

# macOS / Linux
export SSD_KUBECONFIG="$HOME/.kube/config"
export SSD_K8S_NAMESPACE="ssedeploy"
export SSD_DB_PATH="./data/ssedeploy.db"
go run ./cmd/server
```

默认监听 `:8080`，启动时自动创建 `ssedeploy` 命名空间、初始化 SQLite、创建管理员 `admin / admin123`。

验证：

```bash
curl http://127.0.0.1:8080/healthz
curl -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
```

### 3.2 启动前端

```bash
cd frontend
npm install
npm run dev          # 默认 http://127.0.0.1:5173
```

若后端不在 `:8080`，指定代理目标：

```bash
# Windows PowerShell
$env:VITE_API_TARGET="http://127.0.0.1:18080"; npm run dev
# macOS / Linux
VITE_API_TARGET=http://127.0.0.1:18080 npm run dev
```

Vite 已代理 `/api` 与 `/healthz`，并开启 WebSocket 透传（终端、日志流）。

### 3.3 常用环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SSD_HTTP_ADDR` | `:8080` | HTTP 监听地址 |
| `SSD_DB_PATH` | `./data/ssedeploy.db` | SQLite 文件路径 |
| `SSD_JWT_SECRET` | 内置开发密钥 | 生产必须替换 |
| `SSD_TOKEN_TTL` | `24h` | 令牌有效期 |
| `SSD_KUBECONFIG` | 空（优先 InCluster） | kubeconfig 路径 |
| `SSD_K8S_NAMESPACE` | `ssedeploy` | 业务命名空间 |
| `SSD_STORAGE_CLASS` | 空（用集群默认） | PVC StorageClass |
| `SSD_METRICS_INTERVAL` | `15s` | metrics 采样间隔 |
| `SSD_ADMIN_USER` / `SSD_ADMIN_PASSWORD` | `admin` / `admin123` | 首次启动创建的管理员 |

## 4. 容器化一键启动

```bash
# 项目根目录
docker compose up -d --build
```

- 前端：http://127.0.0.1:8000
- 后端：http://127.0.0.1:8080（容器内部经前端 Nginx 反代访问）

**kubeconfig 注意点**：容器内无法通过 `127.0.0.1` / `0.0.0.0` 访问宿主机集群。k3d / kind 的 kubeconfig 中 `server` 通常是 `https://0.0.0.0:PORT`，需要复制一份改写为宿主机地址后挂载：

```bash
# macOS / Linux
sed 's#https://0.0.0.0:#https://host.docker.internal:#' ~/.kube/config > ./kubeconfig.docker.yaml
KUBECONFIG_FILE=./kubeconfig.docker.yaml docker compose up -d --build
```

```powershell
# Windows PowerShell
(Get-Content "$env:USERPROFILE\.kube\config") -replace 'https://0\.0\.0\.0:', 'https://host.docker.internal:' |
  Set-Content .\kubeconfig.docker.yaml
$env:KUBECONFIG_FILE=".\kubeconfig.docker.yaml"; docker compose up -d --build
```

Docker Desktop 的 k3d 端口已映射到宿主机，`host.docker.internal` 默认可直连；若使用 remote context 请改用集群可达地址。

## 5. 在集群内运行（可选）

`deploy/k8s/rbac.yaml` 提供命名空间、ServiceAccount 与 RBAC。若把后端以 Pod 形式部署进集群，将 `SSD_KUBECONFIG` 置空即可走 InCluster 配置：

```bash
kubectl apply -f deploy/k8s/rbac.yaml
# 之后在后端 Deployment 中指定 serviceAccountName: ssedeploy
```

## 6. 接口清单（前缀 `/api/v1`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/auth/login` | 登录，返回 token 与用户信息 |
| GET | `/auth/me` | 当前登录用户 |
| GET | `/pods` | Pod 列表（`keyword`、`status` 过滤） |
| GET | `/pods/stats` | 顶部四个统计数字 |
| POST | `/pods` | 创建 Pod（Deployment + PVC） |
| GET | `/pods/:name` | Pod 详情 |
| PUT | `/pods/:name` | 修改镜像 / 规格 / 环境变量 / 启动命令 |
| DELETE | `/pods/:name` | 删除 Pod（Deployment + PVC + 元数据） |
| POST | `/pods/:name/start` | 启动（副本数 = 1） |
| POST | `/pods/:name/stop` | 停止（副本数 = 0） |
| POST | `/pods/:name/restart` | 滚动重启 |
| GET | `/pods/:name/metrics?minutes=30` | CPU/内存当前值与趋势 |
| GET | `/pods/:name/events` | 最近 Kubernetes 事件 |
| GET | `/pods/:name/logs?tailLines=500` | 历史日志 |
| WS | `/pods/:name/logs/stream` | 实时日志流 |
| WS | `/pods/:name/terminal` | WebSSH 终端 |

响应统一为 `{ code, message, data }`，`code=0` 表示成功。WebSocket 鉴权使用 `?token=<jwt>`。

## 7. 业务规则说明

- **Deployment 承载**：页面上的一切 "Pod" 实际是一个 Deployment（1 副本）+ 一个 PVC（数据盘，挂载 `/data`），名称对用户唯一。
- **启动 / 停止**：启动 = 副本数调到 1，停止 = 副本数调到 0，与截图事件 `Scaled down replica set` 对应。
- **状态映射**：Deployment 不存在 → `NotFound`；副本 0 → `已停止`；容器 `ImagePullBackOff` / `ErrImagePull` / `CrashLoopBackOff` / 探针失败 → `失败`；Pod Running 且 Ready → `运行中`。
- **责任人**：创建者为负责人，角色固定为 `负责人`。
- **列表范围**：仅展示带 `ssedeploy.io/managed=true` 标签的资源，避免误显示集群内其他工作负载。

## 8. 常见问题

| 现象 | 排查方向 |
| --- | --- |
| 监控 tab 无数据 | `kubectl top pods -n ssedeploy` 是否正常；metrics-server 是否加了 `--kubelet-insecure-tls` |
| 创建 Pod 报 PVC 错误 | 集群是否有默认 StorageClass：`kubectl get sc`，或用 `SSD_STORAGE_CLASS` 指定 |
| 终端 / 日志一直连接中 | 反向代理是否开启 WebSocket 升级（Nginx 配置已包含 `Upgrade` 头） |
| 容器内无法访问集群 | kubeconfig 的 `server` 地址是否从容器可达（见第 4 节） |
| 登录 401 | 令牌过期（默认 24h）或 `SSD_JWT_SECRET` 变更，重新登录即可 |
