# =============================================================================
# 清理脚本（Windows / PowerShell 5.1+）
#
# 默认行为（彻底清理，腾出磁盘）：
#   1. 删除容器、网络；
#   2. 删除 4 个数据卷（SQLite、/shared、K3s 数据、kubeconfig）；
#   3. 删除 ssedeploy 业务镜像（K3s 镜像保留，便于下次快速启动）。
#
# 可选开关（保留数据/镜像）：
#   -KeepData    保留数据卷
#   -KeepImages  保留业务镜像
#   -RemoveK3s   同时删除 K3s 镜像（默认保留）
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts\clean.ps1
#   powershell -ExecutionPolicy Bypass -File scripts\clean.ps1 -KeepData
# =============================================================================
param(
  [switch]$KeepData,
  [switch]$KeepImages,
  [switch]$RemoveK3s
)

$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
Set-Location $Root

Write-Host "==> 清理 SSEDeploy 资源" -ForegroundColor Cyan

# 1. 容器与网络
if ($KeepData) {
  docker compose down --remove-orphans
} else {
  Write-Host "    删除数据卷（SQLite / 共享目录 / K3s 状态）" -ForegroundColor Yellow
  docker compose down --remove-orphans -v
}

# 2. 业务镜像
if (-not $KeepImages) {
  $version = "1.0.0"
  if (Test-Path (Join-Path $Root ".env")) {
    $line = Get-Content (Join-Path $Root ".env") | Where-Object { $_ -match "^APP_VERSION=" } | Select-Object -First 1
    if ($line) { $version = ($line -split "=", 2)[1].Trim() }
  }
  foreach ($image in @("ssedeploy/backend:$version", "ssedeploy/frontend:$version")) {
    docker image inspect $image *> $null
    if ($LASTEXITCODE -eq 0) {
      Write-Host "    删除镜像 $image"
      docker rmi $image *> $null
    }
  }
}

# 3. K3s 镜像（默认保留，便于下次启动）
if ($RemoveK3s) {
  docker image inspect "rancher/k3s:v1.35.5-k3s1" *> $null
  if ($LASTEXITCODE -eq 0) {
    Write-Host "    删除 K3s 镜像"
    docker rmi "rancher/k3s:v1.35.5-k3s1" *> $null
  }
}

Write-Host "==> 清理完成" -ForegroundColor Green
if (-not $KeepData) { Write-Host "    注意：平台数据已随数据卷一起删除。" -ForegroundColor Yellow }
