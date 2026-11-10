#!/usr/bin/env bash
# =============================================================================
# 一键停止脚本（Linux / macOS）
#
# 默认：停止并删除容器、网络，保留数据卷
# --remove-data：同时删除数据卷（不可恢复）
#
# 用法：
#   bash scripts/stop.sh
#   bash scripts/stop.sh --remove-data
# =============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REMOVE_DATA=0
[ "${1:-}" = "--remove-data" ] && REMOVE_DATA=1

echo "==> 停止 SSEDeploy 服务"
if [ "$REMOVE_DATA" = "1" ]; then
  echo "    将同时删除数据卷（数据不可恢复）"
  docker compose down --remove-orphans -v
else
  docker compose down --remove-orphans
fi

if [ "$REMOVE_DATA" = "1" ]; then
  echo "==> 已停止，数据卷已删除"
else
  echo "==> 已停止，数据卷保留"
fi
