# =============================================================================
# 开发机打包脚本（Windows / PowerShell 5.1+）
#
# 功能：
#   1. 读取根目录 VERSION 版本号；
#   2. 通过 Docker 多阶段构建自动完成「前端编译 + Go 后端编译 + 镜像构建」；
#   3. 拉取内嵌 K3s 镜像；
#   4. 组装离线交付目录，并导出全部镜像为 tar；
#   5. 生成 .env（写入版本号）与 zip 压缩包。
#
# 用法（在仓库根目录或任意位置执行）：
#   powershell -ExecutionPolicy Bypass -File deploy\offline\scripts\build-package.ps1
#   powershell -ExecutionPolicy Bypass -File deploy\offline\scripts\build-package.ps1 -Version 1.0.1
# =============================================================================
param(
  [string]$Version = "",
  [switch]$NoCache
)

$ErrorActionPreference = "Stop"

# 仓库根目录：scripts -> offline -> deploy -> 根
$RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot "..\..\..")).Path
$OfflineDir = Join-Path $RepoRoot "deploy\offline"

if (-not $Version) {
  $Version = (Get-Content (Join-Path $RepoRoot "VERSION") -Raw).Trim()
}
if (-not $Version) { throw "无法读取版本号，请检查根目录 VERSION 文件" }

$DistRoot = Join-Path $RepoRoot "dist"
$PkgDir = Join-Path $DistRoot "ssedeploy-$Version"
$ImagesDir = Join-Path $PkgDir "images"
$ImageTar = Join-Path $ImagesDir "ssedeploy-images-$Version.tar"

Write-Host "==> SSEDeploy 打包开始，版本：$Version" -ForegroundColor Cyan

# 0. 环境检查
docker info *> $null
if ($LASTEXITCODE -ne 0) { throw "Docker 未就绪，请先启动 Docker Desktop / dockerd" }

# 1. 构建业务镜像（多阶段构建内部完成 npm build 与 go build）
$backedArgs = @("build", "-t", "ssedeploy/backend:$Version", "--build-arg", "APP_VERSION=$Version")
if ($NoCache) { $backedArgs += "--no-cache" }
$backedArgs += (Join-Path $RepoRoot "backend")
Write-Host "==> 构建后端镜像 ssedeploy/backend:$Version"
& docker @backedArgs
if ($LASTEXITCODE -ne 0) { throw "后端镜像构建失败" }

$frontendArgs = @("build", "-t", "ssedeploy/frontend:$Version", "--build-arg", "APP_VERSION=$Version")
if ($NoCache) { $frontendArgs += "--no-cache" }
$frontendArgs += (Join-Path $RepoRoot "frontend")
Write-Host "==> 构建前端镜像 ssedeploy/frontend:$Version"
& docker @frontendArgs
if ($LASTEXITCODE -ne 0) { throw "前端镜像构建失败" }

# 2. 拉取内嵌 K3s 镜像（离线包必须包含，目标机无外网）
$K3sImage = "rancher/k3s:v1.35.5-k3s1"
Write-Host "==> 拉取 K3s 镜像 $K3sImage"
docker pull $K3sImage
if ($LASTEXITCODE -ne 0) { throw "K3s 镜像拉取失败（有网环境才能打包）" }

# 3. 组装交付目录
if (Test-Path $PkgDir) { Remove-Item $PkgDir -Recurse -Force }
New-Item -ItemType Directory -Path $ImagesDir -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $PkgDir "manifests") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $PkgDir "scripts") -Force | Out-Null

Copy-Item (Join-Path $OfflineDir "docker-compose.yml") $PkgDir
# GPU 直通扩展：启动脚本检测到可直通的 NVIDIA GPU 时自动叠加
Copy-Item (Join-Path $OfflineDir "docker-compose.gpu.yml") $PkgDir
Copy-Item (Join-Path $OfflineDir "README.md") $PkgDir
Copy-Item (Join-Path $OfflineDir ".env.example") $PkgDir
Copy-Item (Join-Path $OfflineDir "manifests\shared-pv.yaml") (Join-Path $PkgDir "manifests")
# Docker Hub 镜像加速配置（国内网络创建 Pod 拉取镜像用）
Copy-Item (Join-Path $OfflineDir "manifests\registries.yaml") (Join-Path $PkgDir "manifests")
Set-Content -Path (Join-Path $PkgDir "VERSION") -Value $Version -Encoding ASCII

# 运行时脚本（不包含打包脚本本身）
$runtimeScripts = @(
  "backend-entrypoint.sh",
  "start.ps1", "start.sh",
  "stop.ps1", "stop.sh",
  "load-images.ps1", "load-images.sh",
  "clean.ps1", "clean.sh"
)
foreach ($name in $runtimeScripts) {
  Copy-Item (Join-Path $OfflineDir "scripts\$name") (Join-Path $PkgDir "scripts")
}

# 4. 导出镜像（三个镜像合并为一个 tar，目标机 docker load 一次完成）
Write-Host "==> 导出镜像到 $ImageTar（体积较大，请耐心等待）"
docker save -o $ImageTar "ssedeploy/backend:$Version" "ssedeploy/frontend:$Version" $K3sImage
if ($LASTEXITCODE -ne 0) { throw "镜像导出失败" }
@(
  "ssedeploy/backend:$Version",
  "ssedeploy/frontend:$Version",
  $K3sImage
) | Set-Content -Path (Join-Path $ImagesDir "images.txt") -Encoding ASCII

# 5. 生成 .env（写入版本号，其余保持默认）
# 注意 1：Get-Content 必须显式 -Encoding UTF8，否则 PS 5.1 会按系统 ANSI 读取，中文注释变乱码
# 注意 2：必须使用「UTF-8 无 BOM」写入，带 BOM 会导致 docker compose 解析首个键失败
$envContent = Get-Content (Join-Path $OfflineDir ".env.example") -Raw -Encoding UTF8
$envContent = $envContent -replace "(?m)^APP_VERSION=.*$", "APP_VERSION=$Version"
$envPath = Join-Path $PkgDir ".env"
[System.IO.File]::WriteAllText($envPath, $envContent, (New-Object System.Text.UTF8Encoding($false)))

# 6. 压缩为 zip
$zipPath = Join-Path $DistRoot "ssedeploy-$Version.zip"
if (Test-Path $zipPath) { Remove-Item $zipPath -Force }
Write-Host "==> 压缩离线包 $zipPath"
Compress-Archive -Path $PkgDir -DestinationPath $zipPath -CompressionLevel Optimal

$sizeMB = [math]::Round((Get-Item $zipPath).Length / 1MB, 1)
Write-Host ""
Write-Host "==> 打包完成" -ForegroundColor Green
Write-Host "    目录：$PkgDir"
Write-Host "    压缩包：$zipPath（$sizeMB MB）"
Write-Host "    版本：$Version"
Write-Host "    交付方式：把 zip 拷贝到目标机，解压后执行 scripts\start.ps1（Windows）或 scripts/start.sh（Linux）"
