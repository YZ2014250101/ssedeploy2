#!/usr/bin/env bash
# =============================================================================
# SSEDeploy 一键部署脚本（供云平台 clone 仓库后执行）
#
# 流程：
#   1. 准备 Go / Node 运行时（系统未安装时自动下载到 .tools/）
#   2. 安装前端依赖并构建静态资源（frontend/dist）
#   3. 编译后端二进制（bin/ssedeploy）
#   4. 单进程前台启动：后端同时提供 /api 接口与前端页面
#
# 常用环境变量：
#   PORT                 平台注入的监听端口，默认 8080
#   SSD_KUBECONFIG       目标 K8s 集群凭证；未设置时依次尝试 KUBECONFIG、
#                        ~/.kube/config、仓库根目录 kubeconfig
#   SSD_ADMIN_PASSWORD   初始管理员密码，默认 admin123
#   SSD_JWT_SECRET       生产环境务必替换
#   SSD_K8S_NAMESPACE    业务命名空间，默认 ssedeploy
#   SKIP_INSTALL=1       跳过运行时下载（镜像已内置 Go/Node 时可用）
# =============================================================================
set -eu

ROOT_DIR=$(CDPATH= cd "$(dirname "$0")" && pwd)
cd "$ROOT_DIR"

TOOLS_DIR="$ROOT_DIR/.tools"
BIN_DIR="$ROOT_DIR/bin"
GO_VERSION="${GO_VERSION:-1.26.0}"
NODE_VERSION="${NODE_VERSION:-20.18.0}"
NPM_REGISTRY="${NPM_REGISTRY:-https://registry.npmmirror.com}"

log() { printf '[deploy] %s\n' "$*"; }
die() { printf '[deploy] 错误：%s\n' "$*" >&2; exit 1; }

# download 优先使用 curl，其次 wget
download() {
  url=$1
  dest=$2
  if command -v curl >/dev/null 2>&1; then
    curl -fL --retry 3 --connect-timeout 15 -o "$dest" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$dest" "$url"
  else
    log "未找到 curl/wget，无法下载：$url"
    return 1
  fi
}

case "$(uname -m)" in
  x86_64 | amd64)
    GO_ARCH=amd64
    NODE_ARCH=x64
    ;;
  aarch64 | arm64)
    GO_ARCH=arm64
    NODE_ARCH=arm64
    ;;
  *)
    die "不支持的 CPU 架构：$(uname -m)"
    ;;
esac

ensure_go() {
  if command -v go >/dev/null 2>&1; then
    log "使用系统 Go：$(go version)"
    return 0
  fi

  if [ ! -x "$TOOLS_DIR/go/bin/go" ]; then
    [ "${SKIP_INSTALL:-0}" = "1" ] && die "未找到 go，且已设置 SKIP_INSTALL=1"
    mkdir -p "$TOOLS_DIR"
    tarball="$TOOLS_DIR/go.tar.gz"
    ok=0
    for base in "${GO_DOWNLOAD_BASE:-https://golang.google.cn/dl}" "https://go.dev/dl"; do
      url="$base/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
      log "下载 Go ${GO_VERSION}：$url"
      if download "$url" "$tarball"; then
        tar -xzf "$tarball" -C "$TOOLS_DIR" && rm -f "$tarball" && ok=1 && break
      fi
    done
    [ "$ok" = "1" ] || die "Go ${GO_VERSION} 下载或解压失败，可预装 Go 后重试"
  fi

  export PATH="$TOOLS_DIR/go/bin:$PATH"
  log "使用内置 Go：$(go version)"
}

ensure_node() {
  if command -v node >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
    log "使用系统 Node：$(node -v) / npm $(npm -v)"
    return 0
  fi

  if [ ! -x "$TOOLS_DIR/node/bin/node" ]; then
    [ "${SKIP_INSTALL:-0}" = "1" ] && die "未找到 node/npm，且已设置 SKIP_INSTALL=1"
    mkdir -p "$TOOLS_DIR/node"
    tarball="$TOOLS_DIR/node.tar.gz"
    ok=0
    for base in "${NODE_DOWNLOAD_BASE:-https://npmmirror.com/mirrors/node}" "https://nodejs.org/dist"; do
      url="$base/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.gz"
      log "下载 Node ${NODE_VERSION}：$url"
      if download "$url" "$tarball"; then
        tar -xzf "$tarball" -C "$TOOLS_DIR/node" --strip-components=1 && rm -f "$tarball" && ok=1 && break
      fi
    done
    [ "$ok" = "1" ] || die "Node ${NODE_VERSION} 下载或解压失败，可预装 Node 后重试"
  fi

  export PATH="$TOOLS_DIR/node/bin:$PATH"
  log "使用内置 Node：$(node -v) / npm $(npm -v)"
}

install_frontend_deps() {
  if [ -f package-lock.json ]; then
    set -- ci
  else
    set -- install
  fi
  for registry in "$NPM_REGISTRY" "https://registry.npmjs.org"; do
    log "安装前端依赖（npm $1，registry=$registry）"
    if npm "$1" --no-audit --no-fund --registry="$registry"; then
      return 0
    fi
  done
  die "前端依赖安装失败，请检查网络或预置 frontend/node_modules"
}

build_frontend() {
  log "构建前端静态资源..."
  cd "$ROOT_DIR/frontend"
  if [ ! -d node_modules ]; then
    install_frontend_deps
  fi
  npm run build
  cd "$ROOT_DIR"
  [ -f "$ROOT_DIR/frontend/dist/index.html" ] || die "前端构建失败：缺少 frontend/dist/index.html"
  log "前端构建完成：frontend/dist"
}

build_backend() {
  log "编译后端..."
  export CGO_ENABLED=0
  export GOOS=linux
  export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
  export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
  export GOSUMDB=off
  mkdir -p "$BIN_DIR"
  (cd "$ROOT_DIR/backend" && go build -trimpath -ldflags="-s -w" -o "$BIN_DIR/ssedeploy" ./cmd/server)
  log "后端编译完成：bin/ssedeploy"
}

ensure_go
ensure_node
build_frontend
build_backend

# 端口：优先平台注入的 PORT / APP_PORT
PORT="${PORT:-${APP_PORT:-8080}}"
export SSD_HTTP_ADDR="${SSD_HTTP_ADDR:-:${PORT}}"
export SSD_DB_PATH="${SSD_DB_PATH:-$ROOT_DIR/data/ssedeploy.db}"
export SSD_SHARED_DIR="${SSD_SHARED_DIR:-$ROOT_DIR/shared}"
# 后端同时托管前端页面，单端口即可访问完整平台
export SSD_WEB_DIR="$ROOT_DIR/frontend/dist"
export TZ="${TZ:-Asia/Shanghai}"

mkdir -p "$(dirname "$SSD_DB_PATH")" "$SSD_SHARED_DIR"

# kubeconfig 是平台管理集群的凭证，仓库不包含；缺失时后端无法启动业务功能
if [ -z "${SSD_KUBECONFIG:-}" ] && [ -z "${KUBECONFIG:-}" ] &&
  [ ! -s "$HOME/.kube/config" ] && [ ! -s "$ROOT_DIR/kubeconfig" ]; then
  log "警告：未检测到 kubeconfig，后端将无法连接 Kubernetes 集群。"
  log "      请设置 SSD_KUBECONFIG=/path/to/kubeconfig，或把 kubeconfig 放到仓库根目录。"
fi

log "启动 SSEDeploy：$SSD_HTTP_ADDR（前端目录 $SSD_WEB_DIR）"
exec "$BIN_DIR/ssedeploy"
