#!/usr/bin/env bash
# =============================================================================
# 一键启动脚本（Linux / macOS）
#
# 流程：检查 Docker → 生成 .env → 检查端口 → 导入离线镜像 → 检测 GPU → compose 启动 → 等待前端就绪
#
# 用法：
#   bash scripts/start.sh
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

step() { printf '\033[36m==> %s\033[0m\n' "$1"; }
fail() { printf '\033[31m%s\033[0m\n' "$1" >&2; exit 1; }

# 读取 .env 中的键值（不存在时返回默认值）
env_value() {
  local key="$1" default="$2" line=""
  if [ -f "$ROOT/.env" ]; then
    line="$(grep -E "^\s*${key}\s*=" "$ROOT/.env" | tail -n 1 || true)"
  fi
  if [ -n "$line" ]; then echo "${line#*=}" | tr -d '\r' | xargs; else echo "$default"; fi
}

# 1. Docker 检查
step "检查 Docker"
docker info >/dev/null 2>&1 || fail "Docker 未就绪，请先启动 dockerd 后重试。"

# 2. 配置文件
if [ ! -f "$ROOT/.env" ]; then
  cp "$ROOT/.env.example" "$ROOT/.env"
  step "已从 .env.example 生成 .env（可按需修改端口与密码）"
fi

VERSION="$(env_value APP_VERSION 1.0.0)"
FRONTEND_PORT="$(env_value FRONTEND_PORT 8000)"
BACKEND_PORT="$(env_value BACKEND_PORT 8080)"
K3S_PORT="$(env_value K3S_API_PORT 6443)"
ADMIN_USER="$(env_value ADMIN_USER admin)"
ADMIN_PASSWORD="$(env_value ADMIN_PASSWORD admin123)"

# 3. 端口检查（容器已在运行时跳过）
port_in_use() {
  local port="$1"
  if command -v ss >/dev/null 2>&1; then
    ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${port}$"
  elif command -v netstat >/dev/null 2>&1; then
    netstat -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${port}$"
  else
    return 1
  fi
}

if [ -z "$(docker ps --filter 'name=ssedeploy-frontend' --format '{{.Names}}' 2>/dev/null)" ]; then
  for port in "$FRONTEND_PORT" "$BACKEND_PORT" "$K3S_PORT"; do
    if port_in_use "$port"; then
      fail "端口 $port 已被占用，请修改 .env 中对应端口后重试。"
    fi
  done
fi

# 4. 离线镜像检查与导入
if ! docker image inspect "ssedeploy/backend:${VERSION}" >/dev/null 2>&1; then
  TAR="$(ls "$ROOT"/images/*.tar 2>/dev/null | head -n 1 || true)"
  [ -n "$TAR" ] || fail "本地缺少 ssedeploy/backend:${VERSION} 镜像，且 images 目录下没有离线包。请先执行 scripts/load-images.sh"
  step "导入离线镜像 $(basename "$TAR")（首次启动较慢）"
  docker load -i "$TAR"
fi

# 5. 检测 GPU 直通能力（以容器内能否真正执行 nvidia-smi 为准）
COMPOSE_ARGS=(-f docker-compose.yml)
step "检测 NVIDIA GPU"
if [ ! -f docker-compose.gpu.yml ]; then
  echo "    未找到 docker-compose.gpu.yml，按无 GPU 模式启动"
elif docker run --rm --gpus all --entrypoint nvidia-smi "ssedeploy/backend:${VERSION}" -L >/dev/null 2>&1; then
  COMPOSE_ARGS+=(-f docker-compose.gpu.yml)
  echo "    已检测到 NVIDIA GPU，启用 GPU 直通"
else
  printf '\033[33m    未检测到可直通的 NVIDIA GPU，按无 GPU 模式启动\033[0m\n'
fi

# 6. 启动
step "启动服务（K3s 首次启动需要 1~2 分钟）"
docker compose "${COMPOSE_ARGS[@]}" up -d --remove-orphans || fail "启动失败，请执行 docker compose logs 查看原因。"

# 7. 向 K3s 注入核心镜像（容器内 containerd 与宿主机 Docker 互相独立）
#
# ubuntu:22.04 是平台默认 Pod 镜像：预加载后创建 Pod 直接命中本地镜像，
# 不再访问 docker.io，避免中国大陆网络下拉取超时。
CORE_IMAGES=(
  "rancher/mirrored-pause:3.6"
  "rancher/mirrored-coredns-coredns:1.14.3"
  "rancher/mirrored-metrics-server:v0.8.1"
  "rancher/local-path-provisioner:v0.0.36"
  "rancher/mirrored-library-busybox:1.37.0"
  "ubuntu:22.04"
)
CORE_TAR="$ROOT/images/k3s-core-images.tar"

inject_core_images() {
  local i state="" existing img
  # 容器刚创建时 exec 会失败，先等待进入运行状态
  for i in $(seq 1 60); do
    state="$(docker inspect -f '{{.State.Running}}' ssedeploy-k3s 2>/dev/null || true)"
    [ "$state" = "true" ] && break
    sleep 2
  done
  if [ "$state" != "true" ]; then
    echo "    K3s 容器未运行，跳过镜像注入"
    return 0
  fi

  # 容器内已具备全部核心镜像时直接跳过，避免每次启动重复导入 + 重启
  local need=0
  existing="$(docker exec ssedeploy-k3s ctr -n k8s.io images ls -q 2>/dev/null || true)"
  if [ -n "$existing" ]; then
    for img in "${CORE_IMAGES[@]}"; do
      printf '%s\n' "$existing" | grep -qF "$img" || { need=1; break; }
    done
  else
    need=1
  fi
  if [ "$need" -eq 0 ]; then
    echo "    K3s 内已存在全部核心镜像，跳过导入"
    return 0
  fi

  # 宿主机缺少的镜像尝试拉取（离线环境拉取失败时忽略，只导入已有的）
  local ok=()
  for img in "${CORE_IMAGES[@]}"; do
    if docker image inspect "$img" >/dev/null 2>&1 || docker pull "$img" >/dev/null 2>&1; then
      ok+=("$img")
    else
      echo "    宿主机缺少 $img 且拉取失败，跳过该镜像"
    fi
  done
  if [ "${#ok[@]}" -eq 0 ]; then
    echo "    无可用的核心镜像，跳过注入"
    return 0
  fi

  if [ ! -f "$CORE_TAR" ]; then
    if ! docker save -o "$CORE_TAR" "${ok[@]}" >/dev/null 2>&1; then
      echo "    核心镜像导出失败，跳过注入"
      return 0
    fi
  fi

  local copied=0 imported=0
  for i in $(seq 1 30); do
    if docker cp "$CORE_TAR" ssedeploy-k3s:/tmp/k3s-core-images.tar >/dev/null 2>&1; then
      copied=1
      break
    fi
    sleep 2
  done
  if [ "$copied" -ne 1 ]; then
    echo "    复制镜像包到 K3s 容器失败，跳过注入"
    return 0
  fi

  for i in $(seq 1 30); do
    if docker exec ssedeploy-k3s ctr -n k8s.io images import /tmp/k3s-core-images.tar >/dev/null 2>&1; then
      imported=1
      break
    fi
    sleep 3
  done
  docker exec ssedeploy-k3s rm -f /tmp/k3s-core-images.tar >/dev/null 2>&1 || true

  if [ "$imported" -eq 1 ]; then
    step "重启 K3s 使核心镜像生效"
    docker restart ssedeploy-k3s >/dev/null
  else
    echo "    核心镜像导入失败，请查看 docker logs ssedeploy-k3s"
  fi
}

step "向 K3s 注入核心镜像"
inject_core_images

# 8. 等待前端可访问
step "等待前端就绪 http://127.0.0.1:${FRONTEND_PORT}"
URL="http://127.0.0.1:${FRONTEND_PORT}/"
OK=0
if command -v curl >/dev/null 2>&1; then
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 3 "$URL" >/dev/null 2>&1; then OK=1; break; fi
    sleep 3
  done
fi

echo ""
if [ "$OK" = "1" ]; then
  printf '\033[32m==> 启动成功\033[0m\n'
else
  printf '\033[33m==> 已提交启动，但前端尚未就绪（可能仍在初始化）\033[0m\n'
  echo "    排查：docker compose ps / docker compose logs -f backend k3s"
fi
echo "    版本：$VERSION"
echo "    访问地址：$URL"
echo "    默认账号：${ADMIN_USER} / ${ADMIN_PASSWORD}"
echo "    停止服务：scripts/stop.sh"
