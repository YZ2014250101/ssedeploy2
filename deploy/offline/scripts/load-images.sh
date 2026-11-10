#!/usr/bin/env bash
# =============================================================================
# 离线镜像导入脚本（Linux / macOS）
#
# 用途：在无外网的目标机上导入 images/*.tar。
# 说明：start.sh 在镜像缺失时也会自动导入，本脚本用于手动预导入。
#
# 用法：
#   bash scripts/load-images.sh
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

docker info >/dev/null 2>&1 || { echo "Docker 未就绪" >&2; exit 1; }

shopt -s nullglob
TARS=("$ROOT"/images/*.tar)
if [ ${#TARS[@]} -eq 0 ]; then
  echo "未找到 images/*.tar，请确认离线包完整。" >&2
  exit 1
fi

for tar in "${TARS[@]}"; do
  echo "==> 导入 $(basename "$tar")"
  docker load -i "$tar"
done

echo ""
echo "==> 导入完成，当前镜像："
docker images --format "{{.Repository}}:{{.Tag}}  {{.Size}}" | grep -E "ssedeploy|rancher/k3s" || true
echo "    下一步：bash scripts/start.sh"
