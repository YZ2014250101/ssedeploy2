#!/usr/bin/env bash
# =============================================================================
# 一键启动脚本（Linux / macOS）
#
# 流程：检查 Docker → 生成 .env → 检查端口 → 导入离线镜像 → compose 启动 → 等待前端就绪
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

# 5. 启动
step "启动服务（K3s 首次启动需要 1~2 分钟）"
docker compose up -d --remove-orphans || fail "启动失败，请执行 docker compose logs 查看原因。"

# 6. 等待前端可访问
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
