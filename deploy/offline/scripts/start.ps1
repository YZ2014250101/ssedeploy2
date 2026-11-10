# =============================================================================
# 一键启动脚本（Windows / PowerShell 5.1+）
#
# 流程：
#   1. 检查 Docker 是否就绪；
#   2. 首次运行自动从 .env.example 生成 .env；
#   3. 检查端口占用（可改 .env 中的端口）；
#   4. 本地无镜像时自动从 images/*.tar 导入离线镜像；
#   5. docker compose up -d 启动内嵌 K3s + 后端 + 前端；
#   6. 轮询等待前端可访问后输出访问地址。
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts\start.ps1
# =============================================================================
param(
  [switch]$SkipImageCheck,
  [int]$WaitSeconds = 180
)

$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
Set-Location $Root

function Write-Step($text) { Write-Host "==> $text" -ForegroundColor Cyan }

# 读取 .env 中的键值（不存在时返回默认值）
function Get-EnvValue([string]$key, [string]$default) {
  $file = Join-Path $Root ".env"
  if (Test-Path $file) {
    $line = Get-Content $file | Where-Object { $_ -match "^\s*$([regex]::Escape($key))\s*=" } | Select-Object -First 1
    if ($line) { return ($line -split "=", 2)[1].Trim() }
  }
  return $default
}

# 1. Docker 检查
Write-Step "检查 Docker"
docker info *> $null
if ($LASTEXITCODE -ne 0) {
  Write-Host "Docker 未就绪，请先启动 Docker Desktop（或 Linux 上的 dockerd）后重试。" -ForegroundColor Red
  exit 1
}

# 2. 配置文件
if (-not (Test-Path (Join-Path $Root ".env"))) {
  Copy-Item (Join-Path $Root ".env.example") (Join-Path $Root ".env")
  Write-Step "已从 .env.example 生成 .env（可按需修改端口与密码）"
}

$Version = Get-EnvValue "APP_VERSION" "1.0.0"
$FrontendPort = [int](Get-EnvValue "FRONTEND_PORT" "8000")
$BackendPort = [int](Get-EnvValue "BACKEND_PORT" "8080")
$K3sPort = [int](Get-EnvValue "K3S_API_PORT" "6443")

# 3. 端口检查（容器已在运行时跳过，避免误报）
$running = (docker ps --filter "name=ssedeploy-frontend" --format "{{.Names}}") 2>$null
if (-not $running) {
  foreach ($port in @($FrontendPort, $BackendPort, $K3sPort)) {
    $used = Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue
    if ($used) {
      Write-Host "端口 $port 已被占用，请修改 .env 中对应端口后重试。" -ForegroundColor Red
      exit 1
    }
  }
}

# 4. 离线镜像检查与导入
if (-not $SkipImageCheck) {
  docker image inspect "ssedeploy/backend:$Version" *> $null
  if ($LASTEXITCODE -ne 0) {
    $tar = Get-ChildItem (Join-Path $Root "images\*.tar") -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $tar) {
      Write-Host "本地缺少 ssedeploy/backend:$Version 镜像，且 images 目录下没有离线包。" -ForegroundColor Red
      Write-Host "请先执行 scripts\load-images.ps1，或在有网环境执行 docker compose build。" -ForegroundColor Yellow
      exit 1
    }
    Write-Step "导入离线镜像 $($tar.Name)（首次启动较慢）"
    docker load -i $tar.FullName
    if ($LASTEXITCODE -ne 0) { Write-Host "镜像导入失败" -ForegroundColor Red; exit 1 }
  }
}

# 5. 启动
Write-Step "启动服务（K3s 首次启动需要 1~2 分钟）"
docker compose up -d --remove-orphans
if ($LASTEXITCODE -ne 0) {
  Write-Host "启动失败，请执行 docker compose logs 查看原因。" -ForegroundColor Red
  exit 1
}

# 6. 等待前端可访问
Write-Step "等待前端就绪 http://127.0.0.1:$FrontendPort"
$url = "http://127.0.0.1:$FrontendPort/"
$ok = $false
for ($i = 0; $i -lt $WaitSeconds; $i += 3) {
  try {
    $resp = Invoke-WebRequest -UseBasicParsing -Uri $url -TimeoutSec 3
    if ($resp.StatusCode -eq 200) { $ok = $true; break }
  } catch {
    Start-Sleep -Seconds 3
  }
}

Write-Host ""
if ($ok) {
  Write-Host "==> 启动成功" -ForegroundColor Green
} else {
  Write-Host "==> 已提交启动，但前端尚未就绪（可能仍在拉取/初始化）" -ForegroundColor Yellow
  Write-Host "    排查：docker compose ps / docker compose logs -f backend k3s"
}
Write-Host "    版本：$Version"
Write-Host "    访问地址：$url"
Write-Host "    默认账号：$(Get-EnvValue 'ADMIN_USER' 'admin') / $(Get-EnvValue 'ADMIN_PASSWORD' 'admin123')"
Write-Host "    停止服务：scripts\stop.ps1"
