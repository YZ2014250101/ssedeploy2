# SSEDeploy · 轻量化云原生部署平台（Phase 1：Pod 模块）

对标 YatTerra 的轻量部署控制台。第一阶段只实现 **Pod 模块**，侧边栏其余菜单均为 UI 占位（点击提示「该模块开发中」）。

- 后端：Go + Gin + client-go，RESTful 接口，SQLite 持久化（账号模块使用 GORM）
- 账号认证：支持注册 / 登录，bcrypt 密码加密，JWT 鉴权
- 前端：React 18 + TypeScript + Vite + TailwindCSS + shadcn/ui 风格组件
- 集群交互：Deployment + PVC 管理业务 "Pod"，对用户屏蔽 Deployment 概念

## 1. 目录结构

```
ssedeploy2/
├── backend/                     # Go 后端
│   ├── cmd/server/main.go       # 入口：初始化 DB / K8s / 路由 / 指标采样
│   ├── internal/
│   │   ├── config/              # 环境变量配置
│   │   ├── model/               # User（GORM）/ Pod / Metric / Event 数据模型
│   │   ├── store/               # SQLite 访问层（users / pod_metas / metric_samples）
│   │   ├── service/             # 账号认证业务逻辑（注册校验、bcrypt 加密、事务入库）
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
# 登录（旧路径 /api/v1/auth/login 仍然兼容）
curl -X POST http://127.0.0.1:8080/api/v1/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'

# 注册新账号
curl -X POST http://127.0.0.1:8080/api/v1/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo_user","email":"demo@example.com","password":"demo1234"}'
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
| POST | `/login` | 登录，返回 JWT token 与用户信息（旧路径 `/auth/login` 兼容保留） |
| POST | `/register` | 用户注册 |
| GET | `/auth/me` | 当前登录用户 |
| GET | `/overview` | 概览页聚合数据（别名 `/api/overview`） |
| GET | `/host/:nodeName` | 主机健康详情（`self` 表示后端所在节点，别名 `/api/host/:nodeName`） |
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

### 6.1 概览数据来源

| 卡片字段 | 数据来源 |
| --- | --- |
| 运行 Pod / 集群信息 | client-go 读取集群 Pod、Node、Namespace（无集群级权限时退回业务命名空间） |
| CPU 负载(1m) | 宿主机 `/proc/loadavg`，仅 Linux 可用；Windows/macOS 显示 `--` |
| 内存使用 | Linux：`/proc/meminfo`；Windows：`GlobalMemoryStatusEx` |
| 磁盘使用(/) | Linux：`statfs("/")`；Windows：系统盘 `GetDiskFreeSpaceEx`（容器部署时对应容器根文件系统） |
| GPU 总数与每卡利用率/显存 | 调用后端所在节点的 `nvidia-smi`，未安装或无卡时返回空列表 |
| 攻防攻击/封禁 | 后续阶段接入，当前固定 0 |

前端每 3 秒轮询 `/api/v1/overview` 刷新数据。

### 6.2 主机健康数据来源

`GET /api/v1/host/:nodeName` 返回一次性的实时指标 + 最多 120 个采样点的时序数组（每 3 秒采样一次，保留最近 6 分钟）。

| 页面区块 | 数据来源 |
| --- | --- |
| 系统信息（操作系统 / 内核 / 架构 / CPU 核数 / K3s 版本） | `Node.Status.NodeInfo`、`Node.Status.Capacity` |
| 运行时间 / 负载均值 / Swap | Linux `/proc/uptime`、`/proc/loadavg`、`/proc/meminfo`；Windows 仅运行时间与内存 |
| CPU / 内存 / 磁盘（/）百分比 | Linux `/proc/stat`、`/proc/meminfo`、`statfs("/")`；Windows `GetSystemTimes`、`GlobalMemoryStatusEx` |
| 网络吞吐 / 丢包率 | Linux `/proc/net/dev` 累计计数差值（排除 lo 与虚拟网卡）；Windows `GetIfTable2` 64 位计数 |
| 磁盘 IO / IOPS / await / 繁忙 | Linux `/proc/diskstats` 累计计数差值 |
| 磁盘使用分区列表 | Linux `/proc/mounts` + `statfs` 过滤真实文件系统；Windows 仅系统盘 |
| Pod 数 | client-go `pods`（`spec.nodeName` 字段选择器）统计 Running |
| K3s 节点列表 | `nodes` 的 Ready 与 Disk/Memory/PIDPressure 条件 |
| GPU 总数 / 驱动版本 | 后端所在节点 `nvidia-smi` |

> **多节点限制**：`/proc` 与 `nvidia-smi` 只能采集到后端所在节点的数据。请求非采集器所在节点时，接口仍返回该节点的 Kubernetes Node 信息（系统信息、节点列表、Pod 数），但实时指标与折线图为空，页面会提示「当前节点不是后端所在节点」。

### 6.3 账号认证接口

所有认证接口统一前缀 `/api/v1`，响应统一为 `{ code, message, data }`，`code=0` 表示成功。

#### POST /api/v1/register 用户注册

请求体：

```json
{
  "username": "string",
  "email": "string",
  "password": "string"
}
```

校验规则：

- username：4-20 位，仅字母、数字、下划线，全局唯一
- email：合法邮箱格式，全局唯一
- password：6-24 位
- confirmPassword 仅前端校验，不提交给后端

成功（HTTP 200）：

```json
{
  "code": 0,
  "message": "注册成功",
  "data": {
    "id": 2,
    "username": "demo_user",
    "email": "demo@example.com"
  }
}
```

失败（HTTP 400）：

```json
{
  "code": 400,
  "message": "用户名已被占用",
  "data": null
}
```

其他失败提示：「该邮箱已注册」「用户名需为 4-20 位字母、数字或下划线」「请输入合法的邮箱地址」「密码长度需为 6-24 位」。

#### POST /api/v1/login 用户登录

请求体：

```json
{
  "username": "admin",
  "password": "admin123"
}
```

成功返回 JWT token 与用户信息（密码字段永不返回）：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "user": {
      "id": 1,
      "username": "admin",
      "displayName": "管理员",
      "role": "admin",
      "createdAt": 1730000000
    }
  }
}
```

失败（HTTP 401）：

```json
{
  "code": 401,
  "message": "账号或密码错误",
  "data": null
}
```

登录成功后，后续接口需在请求头携带 `Authorization: Bearer {token}`；WebSocket 使用 `?token={token}`。

## 7. 功能模块说明

### 7.1 账号认证模块

- 支持注册、登录，登录成功后签发 JWT，后续接口通过 `Authorization: Bearer {token}` 鉴权。
- 后端实现位置：`internal/model/user.go`（GORM 模型）、`internal/service/auth.go`（注册业务逻辑）、`internal/handler/auth.go`（登录 / 注册 handler）、`internal/router/router.go`（`/api/v1` 路由组）、`internal/auth/jwt.go`（复用现有 JWT 工具）。
- 注册流程：参数校验 → 用户名 / 邮箱唯一性校验 → bcrypt 加密密码 → GORM 事务入库。
- 登录逻辑与原有实现保持一致：按 username 查用户、bcrypt 比对密码、签发 JWT。

### 7.2 数据库说明

- `users` 表由 GORM `AutoMigrate` 在服务启动时自动创建 / 补全，无需手动执行 SQL。
- 字段：`id`、`username`、`email`、`password_hash`、`display_name`、`role`、`created_at`、`updated_at`。
- `username`、`email` 均建立唯一索引，数据库层面兜底并发重复注册。
- 密码使用 bcrypt 加密后存入 `password_hash`，不保存明文；历史数据库启动时会自动补充 `email`、`updated_at` 字段（可空，兼容存量管理员账号）。

### 7.3 部署说明

- 不需要额外手动建表，程序启动时 GORM 自动迁移生成 / 更新 `users` 表。
- 沿用现有 SQLite 持久化方案（`SSD_DB_PATH`），不新增数据库服务，`docker compose` 与离线包启动方式不变。
- 已有环境升级时直接替换后端镜像并重启即可，历史数据与默认管理员账号保持不变。

### 7.4 前端说明

- 登录页面内置「登录 / 注册账号」Tab 切换，登录 Tab 默认激活，无需单独访问注册页面。
- 注册表单包含用户名、邮箱、密码、确认密码，全部为行内实时校验（防抖 300ms），确认密码不一致时红色提示。
- 密码框带眼睛图标，可在明文 / 密文之间切换，登录与注册表单均支持。
- 注册按钮提交后进入 loading 置灰状态，防止重复提交。
- 注册成功后 toast 提示并自动切回登录 Tab，用户名回填到登录框；注册失败展示后端返回的 `message`。

### 7.5 注意事项

- 密码只保存 bcrypt 密文，任何接口都不返回密码字段。
- 所有认证接口统一前缀 `/api/v1`：登录 `POST /api/v1/login`，注册 `POST /api/v1/register`。
- 登录成功后，后续接口请求 Header 需携带 `Authorization: Bearer {token}`。
- 注册接口的 `confirmPassword` 只做前端校验，不传给后端。

## 8. 业务规则说明

- **Deployment 承载**：页面上的一切 "Pod" 实际是一个 Deployment（1 副本）+ 一个 PVC（数据盘，挂载 `/data`），名称对用户唯一。
- **启动 / 停止**：启动 = 副本数调到 1，停止 = 副本数调到 0，与截图事件 `Scaled down replica set` 对应。
- **状态映射**：Deployment 不存在 → `NotFound`；副本 0 → `已停止`；容器 `ImagePullBackOff` / `ErrImagePull` / `CrashLoopBackOff` / 探针失败 → `失败`；Pod Running 且 Ready → `运行中`。
- **责任人**：创建者为负责人，角色固定为 `负责人`。
- **列表范围**：仅展示带 `ssedeploy.io/managed=true` 标签的资源，避免误显示集群内其他工作负载。

## 9. 常见问题

| 现象 | 排查方向 |
| --- | --- |
| 概览「CPU 负载(1m)」显示 `--` | 负载均值只有 Linux 才提供；容器化部署（Linux 镜像）即可正常显示 |
| 主机页负载/磁盘 IO 曲线全为 0 | Windows 本地不提供负载与磁盘 IO 计数；网络吞吐已通过 `GetIfTable2` 支持，Linux 容器部署后 `/proc` 采集完整生效 |
| 概览没有 GPU 卡片 | 后端所在节点需有 NVIDIA 显卡与驱动，且 `nvidia-smi` 在 PATH 中：`nvidia-smi --query-gpu=index,name --format=csv` |
| 概览 Pod 数与实际不符 | 无集群级 RBAC 时会退回业务命名空间统计，请为 ServiceAccount 补 `nodes` / `namespaces` / `pods` 的 list 权限 |
| 监控 tab 无数据 | `kubectl top pods -n ssedeploy` 是否正常；metrics-server 是否加了 `--kubelet-insecure-tls` |
| 创建 Pod 报 PVC 错误 | 集群是否有默认 StorageClass：`kubectl get sc`，或用 `SSD_STORAGE_CLASS` 指定 |
| 终端 / 日志一直连接中 | 反向代理是否开启 WebSocket 升级（Nginx 配置已包含 `Upgrade` 头） |
| 容器内无法访问集群 | kubeconfig 的 `server` 地址是否从容器可达（见第 4 节） |
| 登录 401 | 令牌过期（默认 24h）或 `SSD_JWT_SECRET` 变更，重新登录即可 |
