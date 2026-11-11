# SSEDeploy 离线交付包 · 使用说明

本离线包把 **前端静态资源 + Go 后端 + 轻量 K3s 集群** 全部封装进 Docker 容器，
目标机器**只需要安装 Docker**，无需 Go / Node / npm / kubectl / K3s，也无需手工配置集群与端口。

- 版本号：见包根目录 `VERSION` 文件
- 默认访问地址：<http://127.0.0.1:8000>
- 默认管理员：`admin / admin123`（首次登录后请立即修改）

---

## 1. 包内结构

```
ssedeploy-<版本>/
├── VERSION                      # 版本标识（升级时对比此文件）
├── README.md                    # 本文档
├── .env                         # 运行配置（端口、密码、版本）
├── .env.example                 # 配置模板
├── docker-compose.yml           # 服务编排（k3s + backend + frontend + bootstrap）
├── docker-compose.gpu.yml       # GPU 直通扩展（启动脚本检测到 NVIDIA GPU 时自动叠加）
├── manifests/
│   └── shared-pv.yaml           # 全局共享目录 /shared 的静态 PV/PVC
├── scripts/
│   ├── backend-entrypoint.sh    # 后端入口：等待并改写 kubeconfig
│   ├── start.ps1 / start.sh     # 一键启动
│   ├── stop.ps1  / stop.sh      # 一键停止（可选删数据）
│   ├── load-images.ps1 / .sh    # 离线镜像导入
│   └── clean.ps1 / clean.sh     # 彻底清理（容器/卷/镜像）
└── images/
    ├── ssedeploy-images-<版本>.tar   # 全部镜像（后端 + 前端 + K3s）
    └── images.txt                     # 镜像清单
```

---

## 2. 目标机要求

| 项目 | 要求 |
| --- | --- |
| Docker | Docker Desktop 4.x（Windows/macOS）或 Docker Engine 20.10+（Linux） |
| Docker Compose | v2（`docker compose` 子命令，随 Docker Desktop 自带） |
| 内存 | 建议 ≥ 8 GB（K3s + 平台服务约占用 2~3 GB） |
| 磁盘 | 建议 ≥ 20 GB 可用空间（镜像约 1.5~2 GB） |
| CPU 虚拟化 | Windows/macOS 需开启虚拟化以运行 Linux 容器 |
| 网络 | **离线可用**；有网环境首次启动会自动拉取缺失镜像 |
| GPU（可选） | NVIDIA 显卡 + 驱动；Docker Desktop 需启用 WSL2 后端与 GPU 支持。启动脚本会自动检测，检测失败则按无 GPU 模式启动 |

> 说明：内嵌 K3s 以特权容器方式运行（`privileged: true`），这是 K3s-in-Docker 的必要条件。

### 2.1 GPU 识别说明

平台概览与 GPU 监控页通过后端容器内的 `nvidia-smi` 采集宿主机显卡，因此需要把 GPU 直通给 `backend`：

- 启动脚本（`start.ps1` / `start.sh`）会执行一次 `docker run --gpus all ... nvidia-smi -L` 探测；
- 探测成功：自动叠加 `docker-compose.gpu.yml`，平台显示真实 GPU 数量、利用率与显存；
- 探测失败：不叠加该文件，平台按无 GPU 模式运行（GPU 数量为 0），不影响其他功能；
- 后端镜像基于 debian-slim，能够执行 NVIDIA 运行时可注入的 `nvidia-smi`（alpine 的 musl 无法执行）。

手动验证 GPU 直通是否可用：

```bash
docker run --rm --gpus all --entrypoint nvidia-smi ssedeploy/backend:<版本> -L
```

能打印显卡列表即表示可用；若提示 `could not select device driver`，请更新 NVIDIA 驱动并确认 Docker Desktop 已开启 GPU 支持。

---

## 3. 快速开始

### 3.1 Windows

```powershell
# 1) 解压离线包，进入目录（以下均在包根目录执行）
# 2) 导入镜像（可选，start 脚本会自动导入）
powershell -ExecutionPolicy Bypass -File scripts\load-images.ps1
# 3) 一键启动
powershell -ExecutionPolicy Bypass -File scripts\start.ps1
# 4) 浏览器访问
start http://127.0.0.1:8000
```

### 3.2 Linux / macOS

```bash
# 1) 解压并进入目录
tar -xzf ssedeploy-<版本>.tar.gz 2>/dev/null || unzip ssedeploy-<版本>.zip
cd ssedeploy-<版本>
# 2) 导入镜像（可选）
bash scripts/load-images.sh
# 3) 一键启动
bash scripts/start.sh
# 4) 访问
# http://<目标机IP>:8000
```

启动脚本会自动完成：Docker 就绪检测 → 生成 `.env` → 端口占用检测 →
导入离线镜像 → 启动 K3s → 初始化共享存储 → 启动后端与前端 → 等待前端可访问。

首次启动 K3s 需要 1~2 分钟，`docker compose ps` 看到 `ssedeploy-backend` 为 `healthy` 即就绪。

---

## 4. 日常运维

| 操作 | 命令（Windows） | 命令（Linux） |
| --- | --- | --- |
| 查看状态 | `docker compose ps` | 同左 |
| 查看日志 | `docker compose logs -f backend` | 同左 |
| 停止（保留数据） | `scripts\stop.ps1` | `bash scripts/stop.sh` |
| 停止并删除数据 | `scripts\stop.ps1 -RemoveData` | `bash scripts/stop.sh --remove-data` |
| 彻底清理（含镜像） | `scripts\clean.ps1` | `bash scripts/clean.sh` |
| 清理但保留数据 | `scripts\clean.ps1 -KeepData` | `bash scripts/clean.sh --keep-data` |

---

## 5. 端口与配置

修改包根目录 `.env` 后重新执行启动脚本（会重建容器）：

```ini
APP_VERSION=1.0.0        # 版本标识
FRONTEND_PORT=8000       # 前端访问端口
BACKEND_PORT=8080        # 后端 API 端口（一般无需暴露）
K3S_API_PORT=6443        # K3s API 端口（仅本机 kubectl 调试）
K8S_NAMESPACE=ssedeploy
ADMIN_USER=admin
ADMIN_PASSWORD=admin123  # 首次启动创建，登录后请修改
JWT_SECRET=...           # 生产环境务必替换为随机长字符串
TZ=Asia/Shanghai
```

若端口被占用，启动脚本会直接报错并提示修改对应端口。

---

## 6. 数据持久化与备份

| Docker 卷 | 内容 | 说明 |
| --- | --- | --- |
| `ssedeploy-data` | SQLite（用户、Pod 元数据） | 重启/升级不丢失 |
| `ssedeploy-shared` | 全局共享目录 `/shared` | 后端与所有用户 Pod 共用 |
| `ssedeploy-k3s-data` | K3s 集群状态、用户 Pod 数据卷 | 删除后 Pod 全部丢失 |
| `ssedeploy-k3s-config` | K3s kubeconfig | 自动生成 |

备份示例（Linux）：

```bash
docker run --rm -v ssedeploy-data:/data -v "$PWD":/backup alpine \
  tar -czf /backup/ssedeploy-data.tar.gz -C /data .
```

恢复时把备份解压回同名卷，再执行启动脚本。

---

## 7. 版本升级

1. 在开发机重新打包：`bash deploy/offline/scripts/build-package.sh 1.0.1`
   （Windows：`powershell -ExecutionPolicy Bypass -File deploy\offline\scripts\build-package.ps1 -Version 1.0.1`）
2. 把新包拷到目标机，**保留原 `.env`**，覆盖 `docker-compose.yml`、`scripts/`、`manifests/`；
3. 导入新镜像后执行 `start.ps1 / start.sh`（`docker compose` 会按新版本号重建容器）；
4. 数据卷名称固定（`name:` 显式声明），升级不会丢数据；
5. 回滚：把 `.env` 的 `APP_VERSION` 改回旧版本，重新启动即可（旧镜像仍在本地时）。

---

## 8. 安全说明

- **路径逃逸防护保留**：IDE 与共享目录的文件接口由后端统一做路径归一化与 `/shared`、
  `/home/workspace` 根目录锁定，`../`、绝对路径、软链接逃逸均被拒绝（403）；
  终端内 `cd ..` 到达根目录后不会再向上跳转。
- IDE 会话按用户隔离，只能访问本人名下 Pod。
- 平台默认使用内置 JWT 密钥，**生产部署请修改 `.env` 中的 `JWT_SECRET` 与管理员密码**。
- K3s API 端口默认仅用于本机调试，不建议在公网暴露 `6443`。

---

## 9. 常见问题

| 现象 | 处理方式 |
| --- | --- |
| 启动脚本提示 Docker 未就绪 | 启动 Docker Desktop / `sudo systemctl start docker` |
| 提示端口被占用 | 修改 `.env` 中 `FRONTEND_PORT` / `BACKEND_PORT` / `K3S_API_PORT` 后重试 |
| 首次启动超过 2 分钟仍未就绪 | `docker compose logs -f k3s` 查看 K3s 是否在初始化镜像/网络 |
| 前端可打开但登录报错 | `docker compose logs backend` 查看数据库与 K3s 连接日志 |
| 监控/GPU 数据为空 | 监控需 metrics-server（K3s 自带）；GPU 页为空先确认宿主机有 NVIDIA 显卡，再执行 `docker run --rm --gpus all --entrypoint nvidia-smi ssedeploy/backend:<版本> -L`；无 GPU 时显示 0 属正常 |
| 内存不足导致 Pod 启动失败 | Docker Desktop 分配内存调至 ≥ 8 GB |
| 换机器后数据不见了 | 数据在 Docker 卷中，需按第 6 节迁移卷，而不是只拷离线包 |
