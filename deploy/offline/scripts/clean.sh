#!/usr/bin/env bash
# =============================================================================
# 清理脚本（Linux / macOS）
#
# 默认：删除容器/网络 + 数据卷 + 业务镜像（K3s 镜像保留）
#   --keep-data    保留数据卷
#   --keep-images  保留业务镜像
#   --remove-k3s   连 K3s 镜像一起删除
#
# 用法：
#   bash scripts/clean.sh
#   bash scripts/clean.sh --keep-data
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

KEEP_DATA=0
KEEP_IMAGES=0
KEEP_K3S=1
for arg in "$@"; do
  case "$arg" in
    --keep-data) KEEP_DATA=1 ;;
    --keep-images) KEEP_IMAGES=1 ;;
    --remove-k3s) KEEP_K3S=0 ;;
    *) echo "未知参数：$arg" >&2; exit 1 ;;
  esac
done

echo "==> 清理 SSEDeploy 资源"

# 1. 容器与网络
if [ "$KEEP_DATA" = "1" ]; then
  docker compose down --remove-orphans
else
  echo "    删除数据卷（SQLite / 共享目录 / K3s 状态）"
  docker compose down --remove-orphans -v
fi

# 2. 业务镜像
VERSION="1.0.0"
if [ -f "$ROOT/.env" ]; then
  VERSION="$(grep -E '^APP_VERSION=' "$ROOT/.env" | tail -n 1 | cut -d= -f2 | tr -d '\r' || echo 1.0.0)"
  [ -n "$VERSION" ] || VERSION="1.0.0"
fi
if [ "$KEEP_IMAGES" = "0" ]; then
  for image in "ssedeploy/backend:${VERSION}" "ssedeploy/frontend:${VERSION}"; do
    if docker image inspect "$image" >/dev/null 2>&1; then
      echo "    删除镜像 $image"
      docker rmi "$image" >/dev/null 2>&1 || true
    fi
  done
fi

# 3. K3s 镜像
if [ "$KEEP_K3S" = "0" ] && docker image inspect "rancher/k3s:v1.35.5-k3s1" >/dev/null 2>&1; then
  echo "    删除 K3s 镜像"
  docker rmi "rancher/k3s:v1.35.5-k3s1" >/dev/null 2>&1 || true
fi

echo "==> 清理完成"
[ "$KEEP_DATA" = "0" ] && echo "    注意：平台数据已随数据卷一起删除。"
