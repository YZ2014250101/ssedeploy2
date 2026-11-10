# =============================================================================
# 一键停止脚本（Windows / PowerShell 5.1+）
#
# 默认：停止并删除容器、网络，保留全部数据卷（用户与 Pod 信息不丢失）
# -RemoveData：同时删除数据卷（SQLite、/shared、K3s 集群状态全部清空，不可恢复）
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts\stop.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\stop.ps1 -RemoveData
# =============================================================================
param(
  [switch]$RemoveData
)

$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
Set-Location $Root

Write-Host "==> 停止 SSEDeploy 服务" -ForegroundColor Cyan
if ($RemoveData) {
  Write-Host "    将同时删除数据卷（数据不可恢复）" -ForegroundColor Yellow
  docker compose down --remove-orphans -v
} else {
  docker compose down --remove-orphans
}

if ($LASTEXITCODE -ne 0) {
  Write-Host "停止失败，请手动执行 docker compose down 排查。" -ForegroundColor Red
  exit 1
}
Write-Host "==> 已停止（镜像与$(if ($RemoveData) { '数据已清理' } else { '数据卷保留' })）" -ForegroundColor Green
