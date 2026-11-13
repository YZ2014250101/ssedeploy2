# =============================================================================
# 一键启动脚本（Windows / PowerShell 5.1+）
#
# 流程：
#   1. 检查 Docker 是否就绪；
#   2. 首次运行自动从 .env.example 生成 .env；
#   3. 检查端口占用（可改 .env 中的端口）；
#   4. 本地无镜像时自动从 images/*.tar 导入离线镜像；
#   5. 准备 K3s 核心镜像（宿主机拉取并导出，供容器内 containerd 离线导入）；
#   6. 检测 NVIDIA GPU 是否可直通（可直通则叠加 docker-compose.gpu.yml）；
#   7. docker compose up -d 启动内嵌 K3s + 后端 + 前端；
#   8. 向 K3s 注入核心镜像并清理僵尸节点 / 卡死 Pod；
#   9. 轮询等待前端可访问后输出访问地址。
#
# 用法：
#   powershell -ExecutionPolicy Bypass -File scripts\start.ps1
# =============================================================================
param(
  [switch]$SkipImageCheck,
  [int]$WaitSeconds = 180
)

# 注意：这里不能设为 Stop。脚本中大量「预期可能失败」的 docker 命令
# （镜像不存在、容器未启动、ctr 尚未就绪等）会把错误写到 stderr，
# PowerShell 5.1 会将其视为终止性错误，导致启动流程被提前中断。
# 关键步骤统一通过 $LASTEXITCODE 判断，失败时显式 exit / 提示。
$ErrorActionPreference = "Continue"
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

# -----------------------------------------------------------------------------
# K3s 系统组件镜像（从 rancher/k3s:v1.35.5-k3s1 内置清单提取）
#
# 容器内的 containerd 与宿主机 Docker 互相独立，无法复用宿主机已拉取的镜像；
# 而 K3s 容器内的 containerd 在部分网络环境下访问 Docker Hub 失败，会导致
# coredns 等系统 Pod 卡在 ContainerCreating。这里先在宿主机准备镜像，
# 再导出为 tar 注入容器内的 containerd。
#
# 升级 K3S_IMAGE 版本时，需同步更新下列标签。
# -----------------------------------------------------------------------------
$CoreImages = @(
  "rancher/mirrored-pause:3.6",                 # Pod 沙箱基础镜像（ContainerCreating 卡住的直接原因）
  "rancher/mirrored-coredns-coredns:1.14.3",    # 集群 DNS
  "rancher/mirrored-metrics-server:v0.8.1",     # 资源指标（监控页数据来源）
  "rancher/local-path-provisioner:v0.0.36",     # 默认存储供给，用户 Pod 的 PVC 依赖它
  "rancher/mirrored-library-busybox:1.37.0",    # 系统辅助镜像
  "ubuntu:22.04",                               # 平台默认 Pod 镜像，预加载后创建 Pod 不再依赖外网
  "mysql:8.0",                                  # 中间件一键部署（MySQL 8.0）
  "minio/minio:RELEASE.2024-06-13T22-53-53Z"    # 中间件一键部署（MinIO S3 对象存储）
)
$ImagesDir = Join-Path $Root "images"
$CoreImagesTar = Join-Path $ImagesDir "k3s-core-images.tar"

# 判断宿主机是否已有指定镜像
function Test-ImageExists([string]$image) {
  docker image inspect $image *> $null
  return ($LASTEXITCODE -eq 0)
}

# 等待容器进入运行状态（K3s 刚创建时 exec 会失败，需要轮询）
function Wait-ContainerRunning([string]$name, [int]$timeoutSec) {
  for ($i = 0; $i -lt $timeoutSec; $i += 2) {
    $state = (docker inspect -f "{{.State.Running}}" $name 2>$null)
    if ($state -eq "true") { return $true }
    Start-Sleep -Seconds 2
  }
  return $false
}

# 等待 K3s API 就绪（容器内 kubectl 读取默认 kubeconfig）
function Wait-K3sReady([int]$timeoutSec) {
  for ($i = 0; $i -lt $timeoutSec; $i += 3) {
    docker exec ssedeploy-k3s kubectl get --raw /readyz *> $null
    if ($LASTEXITCODE -eq 0) { return $true }
    Start-Sleep -Seconds 3
  }
  return $false
}

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

# 5. 准备 K3s 核心镜像（宿主机拉取 → 导出 tar，供容器内 containerd 离线导入）
Write-Step "准备 K3s 核心镜像"
$pulledAny = $false
$availableImages = @()
foreach ($img in $CoreImages) {
  if (Test-ImageExists $img) {
    $availableImages += $img
    continue
  }
  Write-Host "    宿主机缺少 $img，尝试拉取..."
  try {
    docker pull $img
    if ($LASTEXITCODE -eq 0) {
      $availableImages += $img
      $pulledAny = $true
    } else {
      Write-Host "    拉取失败（离线环境可忽略，若 K3s 内已存在该镜像则不影响）：$img" -ForegroundColor Yellow
    }
  } catch {
    Write-Host "    拉取异常（离线环境可忽略）：$img - $($_.Exception.Message)" -ForegroundColor Yellow
  }
}

if (-not (Test-Path $ImagesDir)) {
  New-Item -ItemType Directory -Path $ImagesDir -Force | Out-Null
}

if ($availableImages.Count -gt 0 -and ($pulledAny -or -not (Test-Path $CoreImagesTar))) {
  Write-Step "导出核心镜像到 images\k3s-core-images.tar"
  if (Test-Path $CoreImagesTar) { Remove-Item $CoreImagesTar -Force }
  & docker save -o $CoreImagesTar $availableImages
  if ($LASTEXITCODE -ne 0) {
    Write-Host "    核心镜像导出失败，本次跳过离线注入" -ForegroundColor Yellow
    if (Test-Path $CoreImagesTar) { Remove-Item $CoreImagesTar -Force -ErrorAction SilentlyContinue }
  }
} elseif (Test-Path $CoreImagesTar) {
  Write-Host "    已存在 images\k3s-core-images.tar，跳过导出"
} else {
  Write-Host "    无可用的核心镜像，跳过离线注入（离线环境请预先放置 images\k3s-core-images.tar）" -ForegroundColor Yellow
}

# 6. 检测 GPU 直通能力（以容器内能否真正执行 nvidia-smi 为准）
Write-Step "检测 NVIDIA GPU"
$ComposeFiles = @("-f", "docker-compose.yml")
$GpuOverride = Join-Path $Root "docker-compose.gpu.yml"
if (-not (Test-Path $GpuOverride)) {
  Write-Host "    未找到 docker-compose.gpu.yml，按无 GPU 模式启动"
} else {
  $BackendImage = "ssedeploy/backend:$Version"
  docker run --rm --gpus all --entrypoint nvidia-smi $BackendImage -L *> $null
  if ($LASTEXITCODE -eq 0) {
    $ComposeFiles += @("-f", "docker-compose.gpu.yml")
    Write-Host "    已检测到 NVIDIA GPU，启用 GPU 直通"
  } else {
    Write-Host "    未检测到可直通的 NVIDIA GPU，按无 GPU 模式启动" -ForegroundColor Yellow
  }
}

# 7. 启动
Write-Step "启动服务（K3s 首次启动需要 1~2 分钟）"
docker compose @ComposeFiles up -d --remove-orphans
if ($LASTEXITCODE -ne 0) {
  Write-Host "启动失败，请执行 docker compose logs 查看原因。" -ForegroundColor Red
  exit 1
}

# 8. 向 K3s 容器注入核心镜像（容器内 containerd 与宿主机 Docker 互相独立）
Write-Step "向 K3s 注入核心镜像"
if (-not (Test-Path $CoreImagesTar)) {
  Write-Host "    未找到 images\k3s-core-images.tar，跳过注入" -ForegroundColor Yellow
} elseif (-not (Wait-ContainerRunning "ssedeploy-k3s" 120)) {
  Write-Host "    K3s 容器未运行，跳过镜像注入" -ForegroundColor Yellow
} else {
  # 先检查容器内 containerd 是否已具备全部核心镜像，避免每次启动重复导入 + 重启
  $needImport = $true
  try {
    $existing = @(& docker exec ssedeploy-k3s ctr -n k8s.io images ls -q 2>$null)
    if ($LASTEXITCODE -eq 0 -and $existing.Count -gt 0) {
      $needImport = $false
      foreach ($img in $CoreImages) {
        # ctr 输出形如 docker.io/rancher/mirrored-pause:3.6、docker.io/library/ubuntu:22.04，
        # 直接用原始镜像名做子串匹配，兼容带/不带 registry 前缀两种情况
        if (-not ($existing -match [regex]::Escape($img))) {
          $needImport = $true
          break
        }
      }
    }
  } catch {
    $needImport = $true
  }

  if (-not $needImport) {
    Write-Host "    K3s 内已存在全部核心镜像，跳过导入"
  } else {
    # 容器刚创建时 /tmp 可能尚不可写，复制失败时重试
    $copied = $false
    for ($i = 0; $i -lt 30; $i++) {
      docker cp $CoreImagesTar "ssedeploy-k3s:/tmp/k3s-core-images.tar" *> $null
      if ($LASTEXITCODE -eq 0) { $copied = $true; break }
      Start-Sleep -Seconds 2
    }

    if (-not $copied) {
      Write-Host "    复制镜像包到 K3s 容器失败，跳过注入" -ForegroundColor Yellow
    } else {
      # containerd 就绪需要时间，导入失败时重试
      $imported = $false
      for ($i = 0; $i -lt 30; $i++) {
        docker exec ssedeploy-k3s ctr -n k8s.io images import /tmp/k3s-core-images.tar *> $null
        if ($LASTEXITCODE -eq 0) { $imported = $true; break }
        Start-Sleep -Seconds 3
      }
      docker exec ssedeploy-k3s rm -f /tmp/k3s-core-images.tar *> $null

      if ($imported) {
        Write-Step "重启 K3s 使核心镜像生效"
        docker restart ssedeploy-k3s | Out-Null
      } else {
        Write-Host "    核心镜像导入失败，请查看 docker logs ssedeploy-k3s" -ForegroundColor Yellow
      }
    }
  }
}

# 9. 清理僵尸节点与卡死的 Pod
Write-Step "等待 K3s 就绪"
if (-not (Wait-K3sReady 180)) {
  Write-Host "    K3s 未在预期时间内就绪，跳过清理（可稍后重新执行本脚本）" -ForegroundColor Yellow
} else {
  Write-Step "清理 NotReady 节点与 Terminating Pod"

  # 8.1 删除僵尸节点：节点名默认取自容器 hostname，容器重建后 hostname 变化会残留旧节点
  #     本部署是单节点内嵌 K3s，除当前 hostname 外的节点都是历史残留，可直接删除
  $currentNode = ""
  try {
    $currentNode = ((& docker exec ssedeploy-k3s hostname 2>$null) | Select-Object -First 1)
    if ($currentNode) { $currentNode = $currentNode.Trim() }
  } catch {
    $currentNode = ""
  }

  try {
    $nodeLines = @(& docker exec ssedeploy-k3s kubectl get nodes --no-headers --request-timeout=15s 2>$null)
    foreach ($line in $nodeLines) {
      $cols = @(($line -split "\s+") | Where-Object { $_ })
      if ($cols.Count -lt 2) { continue }
      $nodeName = $cols[0]
      if ($currentNode -and $nodeName -ne $currentNode) {
        Write-Host "    删除僵尸节点：$nodeName（当前节点：$currentNode）"
        & docker exec ssedeploy-k3s kubectl delete node $nodeName --ignore-not-found --request-timeout=15s *> $null
      } elseif (-not $currentNode -and $cols[1] -match "NotReady") {
        # 兜底：取不到 hostname 时只清理 NotReady 节点
        Write-Host "    删除 NotReady 节点：$nodeName"
        & docker exec ssedeploy-k3s kubectl delete node $nodeName --ignore-not-found --request-timeout=15s *> $null
      }
    }
  } catch {
    Write-Host "    节点清理失败：$($_.Exception.Message)" -ForegroundColor Yellow
  }

  # 8.2 清理卡死的 Pod：已进入 Terminating，或仍绑定在已删除节点上的 Pod
  try {
    $validNodes = @(
      & docker exec ssedeploy-k3s kubectl get nodes --no-headers -o custom-columns="NAME:.metadata.name" --request-timeout=15s 2>$null |
        ForEach-Object { $_.Trim() } | Where-Object { $_ }
    )
    $podLines = @(
      & docker exec ssedeploy-k3s kubectl get pods -A --no-headers `
        -o custom-columns="NS:.metadata.namespace,NAME:.metadata.name,NODE:.spec.nodeName,DELETING:.metadata.deletionTimestamp" `
        --request-timeout=15s 2>$null
    )
    foreach ($line in $podLines) {
      $cols = @(($line -split "\s+") | Where-Object { $_ })
      if ($cols.Count -lt 4) { continue }
      $podNs = $cols[0]
      $podName = $cols[1]
      $podNode = $cols[2]
      $terminating = ($cols[3] -ne "<none>")
      $orphan = ($validNodes.Count -gt 0 -and $podNode -and $podNode -ne "<none>" -and $validNodes -notcontains $podNode)
      if ($terminating -or $orphan) {
        Write-Host "    强删卡死 Pod：$podNs/$podName（节点：$podNode）"
        & docker exec ssedeploy-k3s kubectl delete pod $podName -n $podNs --grace-period=0 --force --ignore-not-found --request-timeout=15s *> $null
      }
    }
  } catch {
    Write-Host "    Pod 清理失败：$($_.Exception.Message)" -ForegroundColor Yellow
  }
}

# 10. 等待前端可访问
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
