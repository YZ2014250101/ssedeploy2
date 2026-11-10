# =============================================================================
# 离线镜像导入脚本（Windows / PowerShell 5.1+）
#
# 用途：在没有外网的目标机上，把 images/*.tar 中的镜像导入本地 Docker。
# 说明：start.ps1 在检测到镜像缺失时也会自动调用同样逻辑，本脚本用于手动预导入。
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts\load-images.ps1
# =============================================================================
$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
Set-Location $Root

docker info *> $null
if ($LASTEXITCODE -ne 0) { Write-Host "Docker 未就绪" -ForegroundColor Red; exit 1 }

$tars = Get-ChildItem (Join-Path $Root "images\*.tar") -ErrorAction SilentlyContinue
if (-not $tars) { Write-Host "未找到 images/*.tar，请确认离线包完整。" -ForegroundColor Red; exit 1 }

foreach ($tar in $tars) {
  Write-Host "==> 导入 $($tar.Name)（$([math]::Round($tar.Length / 1MB, 1)) MB）" -ForegroundColor Cyan
  docker load -i $tar.FullName
  if ($LASTEXITCODE -ne 0) { Write-Host "导入失败：$($tar.Name)" -ForegroundColor Red; exit 1 }
}

# 展示导入结果
$version = "1.0.0"
if (Test-Path (Join-Path $Root ".env")) {
  $line = Get-Content (Join-Path $Root ".env") | Where-Object { $_ -match "^APP_VERSION=" } | Select-Object -First 1
  if ($line) { $version = ($line -split "=", 2)[1].Trim() }
}
Write-Host ""
Write-Host "==> 导入完成，当前镜像：" -ForegroundColor Green
docker images --format "{{.Repository}}:{{.Tag}}  {{.Size}}" | Select-String "ssedeploy|rancher/k3s"
Write-Host "    下一步：scripts\start.ps1"
